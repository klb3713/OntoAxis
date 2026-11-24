package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestClientListDocuments(t *testing.T) {
	fixture := Document{ID: uuid.NewString(), PropertiesText: "internal=fixture"}
	for _, tc := range []struct {
		name   string
		body   string
		wantOK bool
	}{
		{"complete", `{"results":[%s],"offset":10000,"limit":1,"total":10001}`, true},
		{"missing results", `{"offset":10000,"limit":1,"total":10000}`, false},
		{"inconsistent page", `{"results":[%s],"offset":0,"limit":1,"total":10001}`, false},
		{"partial page", `{"results":[],"offset":10000,"limit":1,"total":10001}`, false},
		{"missing fields", `{"results":[{"id":"fixture"}],"offset":10000,"limit":1,"total":10001}`, false},
		{"extra field", `{"results":[{"unexpected":"fixture",%s],"offset":10000,"limit":1,"total":10001}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/indexes/active/documents" || r.URL.RawQuery != "limit=1&offset=10000" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				body := tc.body
				if strings.Contains(body, "%s") {
					doc, err := json.Marshal(fixture)
					if err != nil {
						t.Error(err)
						return
					}
					if tc.name == "extra field" {
						doc = doc[1:]
					}
					body = fmt.Sprintf(body, doc)
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			client := newTestClient(t, server)
			got, err := client.ListDocuments(context.Background(), "active", 10000, 1)
			if tc.wantOK {
				if err != nil || got.Total != 10001 || len(got.Documents) != 1 || got.Documents[0].PropertiesText != fixture.PropertiesText {
					t.Fatalf("ListDocuments = %+v, %v", got, err)
				}
			} else if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("ListDocuments error = %v, want ErrUnavailable", err)
			}
			if _, err := client.ListDocuments(context.Background(), "active", -1, 1); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid bounds error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestDocumentDigestCanonical(t *testing.T) {
	first := Document{ID: uuid.NewString(), PropertiesText: "fixture=a", CreatedAt: time.Unix(100, 0).UTC()}
	second := Document{ID: uuid.NewString(), PropertiesText: "fixture=b", CreatedAt: first.CreatedAt.In(time.FixedZone("UTC+8", 8*3600))}
	firstSet, reordered := make(documentHashes), make(documentHashes)
	for _, doc := range []Document{first, second} {
		if err := firstSet.add(doc); err != nil {
			t.Fatal(err)
		}
	}
	second.CreatedAt = second.CreatedAt.UTC()
	for _, doc := range []Document{second, first} {
		if err := reordered.add(doc); err != nil {
			t.Fatal(err)
		}
	}
	if got := firstSet.digest(); len(got) != 64 || got != reordered.digest() {
		t.Fatal("digest must be independent of input ordering and equivalent time zones")
	}
	if err := firstSet.add(first); err == nil {
		t.Fatal("accepted duplicate ID")
	}
	second.PropertiesText = "fixture=drifted"
	delete(reordered, second.ID)
	if err := reordered.add(second); err != nil {
		t.Fatal(err)
	}
	if firstSet.digest() == reordered.digest() {
		t.Fatal("digest ignored same-count content drift")
	}
}

type verifyPageClient struct {
	RebuildClient
	pages map[int]DocumentPage
}

func (f verifyPageClient) ListDocuments(_ context.Context, _ string, offset, _ int) (DocumentPage, error) {
	return f.pages[offset], nil
}

func TestIndexDocumentHashesPagination(t *testing.T) {
	tenant, objectType, otherType := uuid.New(), uuid.New(), uuid.New()
	first := make([]Document, 100)
	for i := range first {
		first[i] = Document{ID: uuid.NewString(), TenantID: tenant.String(), ObjectTypeID: otherType.String()}
	}
	last := Document{ID: uuid.NewString(), TenantID: tenant.String(), ObjectTypeID: objectType.String()}
	for _, tc := range []struct {
		name    string
		last    DocumentPage
		wantErr string
	}{
		{"paged and scoped", DocumentPage{[]Document{last}, 101}, ""},
		{"unstable total", DocumentPage{nil, 100}, "changed while paging"},
		{"duplicate across page boundary", DocumentPage{first[:1], 101}, "duplicate document id"},
		{"wrong tenant", DocumentPage{[]Document{{ID: uuid.NewString(), TenantID: uuid.NewString()}}, 101}, "outside tenant scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := verifyPageClient{pages: map[int]DocumentPage{0: {first, 101}, 100: tc.last}}
			r := Rebuilder{client: f}
			got, err := r.indexDocumentHashes(context.Background(), tenant, &objectType, "fixture")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("indexDocumentHashes error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || len(got) != 1 {
				t.Fatalf("indexDocumentHashes = %v, %v", got, err)
			}
			if _, ok := got[last.ID]; !ok {
				t.Fatal("incorrect scope")
			}
		})
	}
}
