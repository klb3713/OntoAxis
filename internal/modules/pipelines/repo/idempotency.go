package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/klb3713/ontoaxis/internal/modules/pipelines/service"
)

// LookupIdempotency returns an unexpired replay record for the tenant/operation/key.
func (s *PostgresStore) LookupIdempotency(ctx context.Context, tenantID, operation, keyHash string) (service.IdempotencyRecord, bool, error) {
	var record service.IdempotencyRecord
	var body []byte
	err := s.pool.QueryRow(ctx, `
		SELECT tenant_id::text, operation, key_hash, request_hash, response_status, response_body, expires_at
		FROM pipeline.idempotency_keys
		WHERE tenant_id = $1::uuid AND operation = $2 AND key_hash = $3 AND expires_at > now()`,
		tenantID, operation, keyHash).Scan(&record.TenantID, &record.Operation, &record.KeyHash, &record.RequestHash, &record.ResponseStatus, &body, &record.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return service.IdempotencyRecord{}, false, fmt.Errorf("lookup idempotency key: %w", err)
	}
	record.ResponseBody = body
	return record, true, nil
}

// SaveIdempotency inserts a record once and opportunistically removes expired rows.
func (s *PostgresStore) SaveIdempotency(ctx context.Context, record service.IdempotencyRecord) (bool, error) {
	_, _ = s.pool.Exec(ctx, `DELETE FROM pipeline.idempotency_keys WHERE expires_at <= now()`)
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO pipeline.idempotency_keys
			(tenant_id, operation, key_hash, request_hash, response_status, response_body, expires_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (tenant_id, operation, key_hash) DO NOTHING`,
		record.TenantID, record.Operation, record.KeyHash, record.RequestHash, record.ResponseStatus, record.ResponseBody, record.ExpiresAt)
	if err != nil {
		return false, fmt.Errorf("save idempotency key: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
