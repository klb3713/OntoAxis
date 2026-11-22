package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/klb3713/ontoaxis/internal/modules/ontology/domain"
)

func TestMapPgError(t *testing.T) {
	if err := mapPgError(nil); err != nil {
		t.Fatalf("mapPgError(nil) = %v, want nil", err)
	}
	if err := mapPgError(pgx.ErrNoRows); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("mapPgError(pgx.ErrNoRows) = %v, want ErrNotFound", err)
	}
	tests := []struct {
		code string
		want error
	}{
		{"23505", domain.ErrAlreadyExists},
		{"23503", domain.ErrNotFound},
		{"23514", domain.ErrInvalidArgument},
	}
	for _, test := range tests {
		err := mapPgError(&pgconn.PgError{Code: test.code})
		if !errors.Is(err, test.want) {
			t.Errorf("mapPgError(%s) = %v, want %v", test.code, err, test.want)
		}
	}
	unmapped := errors.New("connection reset")
	if err := mapPgError(unmapped); !errors.Is(err, unmapped) {
		t.Fatalf("mapPgError(unmapped) = %v, want original error", err)
	}
}

func TestWithTenantTxRequiresPool(t *testing.T) {
	store := &PostgresStore{}
	err := store.withTenantTx(context.Background(), "tenant-a", func(pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("withTenantTx() error = nil, want pool error")
	}
}

func TestNewReturnsStore(t *testing.T) {
	store := New(nil)
	if store == nil {
		t.Fatal("New(nil) returned nil")
	}
	if store.pool != nil {
		t.Fatal("New(nil) must preserve nil pool")
	}
}
