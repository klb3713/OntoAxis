package db

import (
	"context"
	"testing"
)

func TestOpenRejectsInvalidDSN(t *testing.T) {
	if _, err := Open(context.Background(), "://not-a-dsn"); err == nil {
		t.Fatal("expected invalid dsn error")
	}
}
