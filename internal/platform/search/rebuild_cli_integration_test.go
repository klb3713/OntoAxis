//go:build integration

package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/platform/search"
)

func TestIntegrationRebuildCLIExitAndDigest(t *testing.T) {
	env := testEnv(t)
	tenantID, typeID := env.newTenant(t), newObjectID(t)
	obj := env.insertObject(t, newObjectID(t), tenantID, typeID, map[string]any{"name": "CLI 合成重建夹具"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "ontoaxis-search")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/ontoaxis-search")
	build.Dir = "../../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	run := func(endpoint string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, "rebuild", "--tenant", tenantID.String(), "--verify")
		cmd.Env = append(cmd.Environ(),
			"ONTOAXIS_DATABASE_URL="+strings.TrimSpace(os.Getenv(envDatabaseURL)),
			"ONTOAXIS_SEARCH_ENABLED=true",
			"ONTOAXIS_SEARCH_URL="+endpoint,
			"ONTOAXIS_SEARCH_API_KEY="+env.apiKey,
			"ONTOAXIS_SEARCH_INDEX_VERSION="+env.cfg.IndexVersion,
			"ONTOAXIS_SEARCH_TIMEOUT=10s",
			"ONTOAXIS_LOG_LEVEL=info",
		)
		return cmd.CombinedOutput()
	}
	var firstDigest string
	for i := 0; i < 2; i++ {
		out, err := run(env.meiliURL)
		if err != nil {
			t.Fatalf("rebuild %d: %v\n%s", i+1, err, out)
		}
		var summary struct {
			Message   string `json:"msg"`
			IndexUID  string `json:"index_uid"`
			Documents int64  `json:"documents"`
			Verified  bool   `json:"verified"`
			Digest    string `json:"digest"`
		}
		for _, line := range strings.Split(string(out), "\n") {
			if json.Unmarshal([]byte(line), &summary) == nil && summary.Message == "search rebuild complete" {
				break
			}
		}
		if summary.Message != "search rebuild complete" || !summary.Verified || summary.Documents != 1 ||
			summary.IndexUID != env.indexUID(tenantID) || len(summary.Digest) != 64 {
			t.Fatalf("unexpected CLI summary: %+v", summary)
		}
		if i == 0 {
			firstDigest = summary.Digest
		} else if summary.Digest != firstDigest {
			t.Fatal("CLI digest changed between consecutive rebuilds")
		}
	}
	t.Log("two independent CLI rebuilds exited 0 with the same verified digest")

	// Inject actual drift into the real Meilisearch instance immediately before
	// the CLI reads back the active index. All HTTP responses come from Meili.
	base, err := url.Parse(env.meiliURL)
	if err != nil {
		t.Fatal("invalid integration Meilisearch URL")
	}
	drifted, err := search.BuildDocument(obj)
	if err != nil {
		t.Fatal(err)
	}
	drifted.PropertiesText = "fixture=cli-same-count-drift"
	proxy := httputil.NewSingleHostReverseProxy(base)
	var once sync.Once
	var injectErr error
	indexUID := env.indexUID(tenantID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/indexes/"+indexUID+"/documents" {
			once.Do(func() {
				var taskID string
				taskID, injectErr = env.client.UpsertDocuments(r.Context(), indexUID, []search.Document{drifted})
				if injectErr == nil {
					injectErr = env.client.WaitTask(r.Context(), indexUID, taskID, 5*time.Second)
				}
			})
			if injectErr != nil {
				http.Error(w, "fixture injection failed", http.StatusBadGateway)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	out, err := run(server.URL)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !strings.Contains(string(out), "document digest mismatch") ||
		strings.Contains(string(out), drifted.PropertiesText) || strings.Contains(string(out), "search rebuild complete") {
		t.Fatalf("CLI must exit 1 on same-count drift without printing document content: %v\n%s", err, out)
	}
	t.Log("CLI exited 1 for same-count document drift and reported only digest differences")
}
