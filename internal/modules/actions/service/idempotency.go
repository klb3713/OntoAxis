package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
)

type idempotencyContextKey struct{}

// WithIdempotencyKey attaches an API-supplied idempotency key to the request
// context. The service treats an empty key as "idempotency not requested" so
// existing callers keep their previous behavior.
func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyContextKey{}, key)
}

func idempotencyKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyContextKey{}).(string)
	return key
}

// idempotencyRequestHash binds a key to the submitted action type and payload.
// Same key with a different hash is rejected instead of replaying a different
// execution.
func idempotencyRequestHash(actionTypeID uuid.UUID, parameters map[string]any) string {
	payload, _ := json.Marshal(map[string]any{
		"action_type_id": actionTypeID.String(),
		"parameters":     parameters,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
