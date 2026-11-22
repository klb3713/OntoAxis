package authn

import (
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// RemoteKeySource fetches an RS256 JWKS document and caches it. The cache is
// intentionally short-lived so key rotation is observed quickly; forceRefresh
// bypasses it once for an unknown kid.
type RemoteKeySource struct {
	URL    string
	Client *http.Client
	TTL    time.Duration
	Now    func() time.Time

	mu      sync.Mutex
	keys    KeySet
	fetched time.Time
}

// NewRemoteKeySource constructs a JWKS source with a minimal HTTP client.
func NewRemoteKeySource(url string) *RemoteKeySource {
	return &RemoteKeySource{
		URL:    url,
		Client: &http.Client{Timeout: 5 * time.Second},
		TTL:    5 * time.Minute,
		Now:    time.Now,
	}
}

// Keys implements KeySource.
func (s *RemoteKeySource) Keys(ctx context.Context, forceRefresh bool) (KeySet, error) {
	if s == nil || s.URL == "" {
		return nil, fmt.Errorf("%w: JWKS URL is not configured", ErrUnauthenticated)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if !forceRefresh && s.keys != nil && now.Sub(s.fetched) < s.TTL {
		return s.keys, nil
	}
	keys, err := s.fetch(ctx)
	if err != nil {
		return nil, err
	}
	s.keys = keys
	s.fetched = now
	return keys, nil
}

func (s *RemoteKeySource) fetch(ctx context.Context) (KeySet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build JWKS request: %v", ErrUnauthenticated, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch JWKS: %v", ErrUnauthenticated, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: JWKS returned HTTP %d", ErrUnauthenticated, resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			KID string `json:"kid"`
			KTY string `json:"kty"`
			Alg string `json:"alg"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: decode JWKS: %v", ErrUnauthenticated, err)
	}
	out := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, key := range doc.Keys {
		if key.KID == "" || key.KTY != "RSA" || (key.Alg != "" && key.Alg != "RS256") {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			continue
		}
		if len(eBytes) == 0 || len(eBytes) > 4 {
			continue
		}
		var e int
		for _, b := range eBytes {
			e = e<<8 | int(b)
		}
		if e < 3 {
			continue
		}
		out[key.KID] = &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}
	}
	generic := make(map[string]crypto.PublicKey, len(out))
	for kid, key := range out {
		generic[kid] = key
	}
	return NewStaticKeySet(generic), nil
}
