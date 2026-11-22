package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/klb3713/ontoaxis/internal/modules/datasets/domain"
)

func TestPostgresStoreFailsClosedWithoutPool(t *testing.T) {
	ctx := context.Background()
	store := NewPostgresStore(nil)
	tenantID := testID(t)
	datasetID := testID(t)

	if err := store.Ping(ctx); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Ping() error = %v, want ErrUnavailable", err)
	}
	if _, err := store.GetDataset(ctx, tenantID, datasetID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("GetDataset() error = %v, want ErrUnavailable", err)
	}
	if _, _, err := store.ListDatasets(ctx, tenantID, 0, 10); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ListDatasets() error = %v, want ErrUnavailable", err)
	}
	if err := store.DeleteDataset(ctx, tenantID, datasetID, time.Now().UTC()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("DeleteDataset() error = %v, want ErrUnavailable", err)
	}
}
