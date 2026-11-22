package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteKeySourceFetchesAndCaches(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kid": "test", "kty": "RSA", "alg": "RS256", "n": n, "e": e,
		}}})
	}))
	defer srv.Close()

	src := NewRemoteKeySource(srv.URL)
	src.Now = func() time.Time { return time.Unix(100, 0) }
	keys, err := src.Keys(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := keys.LookupKey(context.Background(), "test"); !ok {
		t.Fatal("expected key")
	}
	if _, err := src.Keys(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one fetch, got %d", calls.Load())
	}
	if _, err := src.Keys(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected force refresh, got %d", calls.Load())
	}
}
