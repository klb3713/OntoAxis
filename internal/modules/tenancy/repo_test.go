package tenancy

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresStoreConstruction(t *testing.T) {
	store := NewPostgresStore(nil)
	if store == nil {
		t.Fatal("NewPostgresStore(nil) = nil")
	}
	if store.pool != nil {
		t.Fatal("NewPostgresStore(nil).pool != nil")
	}
}

func TestPgUUIDRoundTrip(t *testing.T) {
	id := uuid.MustParse("11111111-1111-7111-8111-111111111111")
	got := fromPgUUID(pgUUID(id))
	if got != id {
		t.Fatalf("fromPgUUID(pgUUID(%s)) = %s", id, got)
	}
	if pgUUID(uuid.Nil).Valid {
		t.Fatal("pgUUID(uuid.Nil) is valid, want invalid")
	}
}

func TestStoreErrMappings(t *testing.T) {
	other := errors.New("connection reset")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "no rows", err: pgx.ErrNoRows, want: ErrNotFound},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505"}, want: ErrConflict},
		{name: "foreign key violation", err: &pgconn.PgError{Code: "23503"}, want: ErrConflict},
		{name: "unmapped", err: other, want: other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storeErr(tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("storeErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
	if storeErr(nil) != nil {
		t.Fatal("storeErr(nil) != nil")
	}
}
