package authn

import (
	"context"
	"sync"
)

// FakeVerifier is a deterministic Verifier for unit tests and non-production
// assembly. It never performs cryptography and must not be used in production.
type FakeVerifier struct {
	Claims Claims
	Err    error

	mu        sync.Mutex
	calls     int
	lastToken string
}

// Verify implements Verifier.
func (f *FakeVerifier) Verify(_ context.Context, rawToken string) (Claims, error) {
	f.mu.Lock()
	f.calls++
	f.lastToken = rawToken
	f.mu.Unlock()

	if f.Err != nil {
		return Claims{}, f.Err
	}
	return f.Claims, nil
}

// Calls reports how many times Verify was invoked.
func (f *FakeVerifier) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// LastToken reports the most recent token passed to Verify.
func (f *FakeVerifier) LastToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastToken
}

var _ Verifier = (*FakeVerifier)(nil)
