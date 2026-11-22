package authn

import (
	"context"
	"crypto"
)

// KeySet resolves a signing key by JWT key ID.
type KeySet interface {
	LookupKey(ctx context.Context, kid string) (crypto.PublicKey, bool)
}

// KeySource loads the current key set. Implementations own the JWKS cache
// (five minutes per ARCHITECTURE.md 6.3.1); forceRefresh must bypass it.
type KeySource interface {
	Keys(ctx context.Context, forceRefresh bool) (KeySet, error)
}

// StaticKeySet is an immutable in-memory key set, useful for tests and for
// small deployments that provide keys out of band.
type StaticKeySet struct {
	keys map[string]crypto.PublicKey
}

// NewStaticKeySet copies keys into an immutable KeySet.
func NewStaticKeySet(keys map[string]crypto.PublicKey) *StaticKeySet {
	copied := make(map[string]crypto.PublicKey, len(keys))
	for kid, key := range keys {
		copied[kid] = key
	}
	return &StaticKeySet{keys: copied}
}

// LookupKey implements KeySet.
func (s *StaticKeySet) LookupKey(_ context.Context, kid string) (crypto.PublicKey, bool) {
	if s == nil {
		return nil, false
	}
	key, ok := s.keys[kid]
	return key, ok
}

// StaticKeySource always returns the same static key set.
type StaticKeySource struct {
	keys map[string]crypto.PublicKey
}

// NewStaticKeySource copies keys into a reusable source.
func NewStaticKeySource(keys map[string]crypto.PublicKey) *StaticKeySource {
	copied := make(map[string]crypto.PublicKey, len(keys))
	for kid, key := range keys {
		copied[kid] = key
	}
	return &StaticKeySource{keys: copied}
}

// Keys implements KeySource.
func (s *StaticKeySource) Keys(_ context.Context, _ bool) (KeySet, error) {
	if s == nil {
		return NewStaticKeySet(nil), nil
	}
	return NewStaticKeySet(s.keys), nil
}

var _ KeySet = (*StaticKeySet)(nil)
var _ KeySource = (*StaticKeySource)(nil)
