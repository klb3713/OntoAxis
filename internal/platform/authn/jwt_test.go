package authn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type rsaTestKey struct {
	kid     string
	private *rsa.PrivateKey
	public  *rsa.PublicKey
}

func newRSATestKey(t *testing.T, kid string) *rsaTestKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return &rsaTestKey{kid: kid, private: private, public: &private.PublicKey}
}

func encodeSegment(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func signTestToken(t *testing.T, key *rsaTestKey, header, claims map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	signingInput := encodeSegment(headerJSON) + "." + encodeSegment(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key.private, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signingInput + "." + encodeSegment(signature)
}

func defaultTestClaims(now time.Time) map[string]any {
	return map[string]any{
		"iss":           DefaultIssuer,
		"aud":           DefaultAudience,
		"sub":           "22222222-2222-2222-2222-222222222222",
		"tenant_id":     "11111111-1111-1111-1111-111111111111",
		"org_ids":       []string{"33333333-3333-3333-3333-333333333333"},
		"workspace_ids": []string{"44444444-4444-4444-4444-444444444444"},
		"roles":         []string{"member", "viewer"},
		"email":         "user@example.com",
		"name":          "Test User",
		"exp":           now.Add(5 * time.Minute).Unix(),
		"iat":           now.Unix(),
		"auth_time":     now.Unix(),
	}
}

func testJWKSVerifier(keys map[string]crypto.PublicKey, now time.Time) *JWKSVerifier {
	return &JWKSVerifier{
		Issuer:       DefaultIssuer,
		Audience:     DefaultAudience,
		Keys:         NewStaticKeySource(keys),
		Now:          func() time.Time { return now },
		MaxClockSkew: 30 * time.Second,
	}
}

func TestJWKSVerifierValidToken(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid, "typ": "JWT"}, defaultTestClaims(now))

	claims, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if claims.Subject != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("Subject = %q", claims.Subject)
	}
	if claims.TenantID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("TenantID = %q", claims.TenantID)
	}
	if claims.Email != "user@example.com" {
		t.Fatalf("Email = %q", claims.Email)
	}
	if len(claims.Audience) != 1 || claims.Audience[0] != DefaultAudience {
		t.Fatalf("Audience = %v", claims.Audience)
	}
}

func TestJWKSVerifierAudienceArray(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	claims := defaultTestClaims(now)
	claims["aud"] = []string{"other", DefaultAudience}
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, claims)

	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify with audience array error: %v", err)
	}
}

func TestJWKSVerifierRejectsUnsupportedAlgorithms(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	payload, err := json.Marshal(defaultTestClaims(now))
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	for _, alg := range []string{"none", "HS256", "RS512"} {
		t.Run(alg, func(t *testing.T) {
			header, _ := json.Marshal(map[string]any{"alg": alg, "kid": key.kid})
			token := encodeSegment(header) + "." + encodeSegment(payload) + "."
			if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestJWKSVerifierRejectsTamperedSignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	trusted := newRSATestKey(t, "kid-1")
	attacker := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{trusted.kid: trusted.public}, now)
	token := signTestToken(t, attacker, map[string]any{"alg": "RS256", "kid": "kid-1"}, defaultTestClaims(now))

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierRejectsWrongIssuer(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	claims := defaultTestClaims(now)
	claims["iss"] = "https://evil.example/realms/ontoaxis"
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, claims)

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierRejectsWrongAudience(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	claims := defaultTestClaims(now)
	claims["aud"] = "other-api"
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, claims)

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierClockSkew(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)

	withinSkew := defaultTestClaims(now)
	withinSkew["exp"] = now.Add(-10 * time.Second).Unix()
	if _, err := verifier.Verify(context.Background(), signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, withinSkew)); err != nil {
		t.Fatalf("token within clock skew rejected: %v", err)
	}

	expired := defaultTestClaims(now)
	expired["exp"] = now.Add(-31 * time.Second).Unix()
	if _, err := verifier.Verify(context.Background(), signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, expired)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired token error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierRejectsMissingExp(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	claims := defaultTestClaims(now)
	delete(claims, "exp")
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, claims)

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierRejectsFutureIssuedAt(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)
	claims := defaultTestClaims(now)
	claims["iat"] = now.Add(31 * time.Second).Unix()
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, claims)

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
}

func TestJWKSVerifierRejectsMalformedTokens(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	verifier := testJWKSVerifier(map[string]crypto.PublicKey{key.kid: key.public}, now)

	for _, token := range []string{"", "a.b", "a.b.c.d", strings.Repeat("a", maxTokenSize+1)} {
		if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q error = %v, want ErrUnauthenticated", token, err)
		}
	}
}

type rotatingKeySource struct {
	mu        sync.Mutex
	initial   map[string]crypto.PublicKey
	refreshed map[string]crypto.PublicKey
	calls     []bool
}

func (s *rotatingKeySource) Keys(_ context.Context, forceRefresh bool) (KeySet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, forceRefresh)
	if forceRefresh {
		return NewStaticKeySet(s.refreshed), nil
	}
	return NewStaticKeySet(s.initial), nil
}

func TestJWKSVerifierUnknownKidRefreshesOnce(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-new")
	source := &rotatingKeySource{
		initial:   map[string]crypto.PublicKey{"kid-old": newRSATestKey(t, "kid-old").public},
		refreshed: map[string]crypto.PublicKey{key.kid: key.public},
	}
	verifier := testJWKSVerifier(nil, now)
	verifier.Keys = source
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, defaultTestClaims(now))

	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify after refresh error: %v", err)
	}
	if len(source.calls) != 2 || source.calls[0] != false || source.calls[1] != true {
		t.Fatalf("key source calls = %v, want [false true]", source.calls)
	}
}

func TestJWKSVerifierUnknownKidStillFails(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-missing")
	source := &rotatingKeySource{
		initial:   map[string]crypto.PublicKey{"kid-old": newRSATestKey(t, "kid-old").public},
		refreshed: map[string]crypto.PublicKey{"kid-other": newRSATestKey(t, "kid-other").public},
	}
	verifier := testJWKSVerifier(nil, now)
	verifier.Keys = source
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, defaultTestClaims(now))

	if _, err := verifier.Verify(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Verify error = %v, want ErrUnauthenticated", err)
	}
	if len(source.calls) != 2 {
		t.Fatalf("key source calls = %v, want two attempts", source.calls)
	}
}

func TestJWKSVerifierCachedKeyNoRefresh(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := newRSATestKey(t, "kid-1")
	source := &rotatingKeySource{initial: map[string]crypto.PublicKey{key.kid: key.public}}
	verifier := testJWKSVerifier(nil, now)
	verifier.Keys = source
	token := signTestToken(t, key, map[string]any{"alg": "RS256", "kid": key.kid}, defaultTestClaims(now))

	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if len(source.calls) != 1 || source.calls[0] != false {
		t.Fatalf("key source calls = %v, want [false]", source.calls)
	}
}
