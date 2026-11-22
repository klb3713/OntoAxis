package authn

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	maxTokenSize   = 128 * 1024
	maxHeaderSize  = 16 * 1024
	defaultSkew    = 30 * time.Second
	allowedJWKSAlg = "RS256"
)

// Verifier validates a raw JWT and returns its verified claims.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (Claims, error)
}

// JWKSVerifier implements a compact JWS RS256 verifier. It deliberately
// supports only RS256 and rejects alg=none, HMAC algorithms, and every other
// algorithm before any key material is consulted.
type JWKSVerifier struct {
	Issuer       string
	Audience     string
	Keys         KeySource
	Now          func() time.Time
	MaxClockSkew time.Duration
}

// NewJWKSVerifier constructs a verifier with the architecture defaults.
func NewJWKSVerifier(issuer, audience string, keys KeySource) *JWKSVerifier {
	if issuer == "" {
		issuer = DefaultIssuer
	}
	if audience == "" {
		audience = DefaultAudience
	}
	return &JWKSVerifier{
		Issuer:       issuer,
		Audience:     audience,
		Keys:         keys,
		Now:          time.Now,
		MaxClockSkew: defaultSkew,
	}
}

// Verify implements Verifier.
func (v *JWKSVerifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	if v == nil {
		return Claims{}, fmt.Errorf("%w: verifier is not configured", ErrUnauthenticated)
	}
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return Claims{}, fmt.Errorf("%w: token is empty", ErrUnauthenticated)
	}
	if len(token) > maxTokenSize {
		return Claims{}, fmt.Errorf("%w: token is too large", ErrUnauthenticated)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, fmt.Errorf("%w: token is not a compact JWS", ErrUnauthenticated)
	}

	headerBytes, err := decodeSegment(parts[0], maxHeaderSize)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: invalid token header", ErrUnauthenticated)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Claims{}, fmt.Errorf("%w: invalid token header", ErrUnauthenticated)
	}
	if header.Alg != allowedJWKSAlg {
		return Claims{}, fmt.Errorf("%w: unsupported signing algorithm", ErrUnauthenticated)
	}
	kid := strings.TrimSpace(header.Kid)
	if kid == "" {
		return Claims{}, fmt.Errorf("%w: token is missing kid", ErrUnauthenticated)
	}

	key, err := v.lookupKey(ctx, kid)
	if err != nil {
		return Claims{}, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return Claims{}, fmt.Errorf("%w: signing key is not RSA", ErrUnauthenticated)
	}
	signature, err := decodeSegment(parts[2], maxTokenSize)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: invalid token signature", ErrUnauthenticated)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, digest[:], signature); err != nil {
		return Claims{}, fmt.Errorf("%w: token signature is invalid", ErrUnauthenticated)
	}

	payload, err := decodeSegment(parts[1], maxTokenSize)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: invalid token payload", ErrUnauthenticated)
	}
	claims, err := parseClaims(payload)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: invalid token claims", ErrUnauthenticated)
	}
	if err := v.validateClaims(claims); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

func (v *JWKSVerifier) lookupKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	if v.Keys == nil {
		return nil, fmt.Errorf("%w: no key source configured", ErrUnauthenticated)
	}
	keys, err := v.Keys.Keys(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot load signing keys", ErrUnauthenticated)
	}
	if key, ok := lookupInSet(ctx, keys, kid); ok {
		return key, nil
	}
	// Unknown kid: force one refresh, then fail if the key is still absent.
	keys, err = v.Keys.Keys(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot refresh signing keys", ErrUnauthenticated)
	}
	if key, ok := lookupInSet(ctx, keys, kid); ok {
		return key, nil
	}
	return nil, fmt.Errorf("%w: unknown kid", ErrUnauthenticated)
}

func lookupInSet(ctx context.Context, keys KeySet, kid string) (crypto.PublicKey, bool) {
	if keys == nil {
		return nil, false
	}
	return keys.LookupKey(ctx, kid)
}

func decodeSegment(segment string, maxSize int) ([]byte, error) {
	if segment == "" || len(segment) > maxSize {
		return nil, fmt.Errorf("invalid segment size")
	}
	return base64.RawURLEncoding.DecodeString(segment)
}

type jwtClaimsPayload struct {
	Issuer       string          `json:"iss"`
	Audience     json.RawMessage `json:"aud"`
	Subject      string          `json:"sub"`
	TenantID     string          `json:"tenant_id"`
	OrgIDs       []string        `json:"org_ids"`
	WorkspaceIDs []string        `json:"workspace_ids"`
	Roles        []string        `json:"roles"`
	Email        string          `json:"email"`
	Name         string          `json:"name"`
	ExpiresAt    json.Number     `json:"exp"`
	IssuedAt     json.Number     `json:"iat"`
	AuthTime     json.Number     `json:"auth_time"`
}

func parseClaims(payload []byte) (Claims, error) {
	var raw jwtClaimsPayload
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return Claims{}, err
	}
	audience, err := parseAudience(raw.Audience)
	if err != nil {
		return Claims{}, err
	}
	expiresAt, _, err := parseNumericDate(raw.ExpiresAt)
	if err != nil {
		return Claims{}, err
	}
	issuedAt, _, err := parseNumericDate(raw.IssuedAt)
	if err != nil {
		return Claims{}, err
	}
	authTime, _, err := parseNumericDate(raw.AuthTime)
	if err != nil {
		return Claims{}, err
	}
	return Claims{
		Issuer:       raw.Issuer,
		Audience:     audience,
		Subject:      raw.Subject,
		TenantID:     raw.TenantID,
		OrgIDs:       raw.OrgIDs,
		WorkspaceIDs: raw.WorkspaceIDs,
		Roles:        raw.Roles,
		Email:        raw.Email,
		Name:         raw.Name,
		ExpiresAt:    expiresAt,
		IssuedAt:     issuedAt,
		AuthTime:     authTime,
	}, nil
}

func parseAudience(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var single string
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, err
		}
		return []string{single}, nil
	}
	var list []string
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func parseNumericDate(value json.Number) (time.Time, bool, error) {
	if value == "" {
		return time.Time{}, false, nil
	}
	seconds, err := value.Float64()
	if err != nil {
		return time.Time{}, false, err
	}
	whole := int64(seconds)
	nanos := int64((seconds - float64(whole)) * 1e9)
	return time.Unix(whole, nanos), true, nil
}

func (v *JWKSVerifier) validateClaims(claims Claims) error {
	if claims.Issuer != v.Issuer {
		return fmt.Errorf("%w: token issuer is not trusted", ErrUnauthenticated)
	}
	audienceOK := false
	for _, audience := range claims.Audience {
		if audience == v.Audience {
			audienceOK = true
			break
		}
	}
	if !audienceOK {
		return fmt.Errorf("%w: token audience is not accepted", ErrUnauthenticated)
	}
	if claims.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: exp claim is required", ErrUnauthenticated)
	}
	now := v.now()
	skew := v.MaxClockSkew
	if skew <= 0 {
		skew = defaultSkew
	}
	if now.After(claims.ExpiresAt.Add(skew)) {
		return fmt.Errorf("%w: token is expired", ErrUnauthenticated)
	}
	if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Add(skew)) {
		return fmt.Errorf("%w: token was issued in the future", ErrUnauthenticated)
	}
	return nil
}

func (v *JWKSVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}
