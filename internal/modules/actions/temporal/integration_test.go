//go:build integration

package temporal_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/klb3713/ontoaxis/internal/modules/actions/repo"
	"github.com/klb3713/ontoaxis/internal/modules/actions/service"
	actiontemporal "github.com/klb3713/ontoaxis/internal/modules/actions/temporal"
	actionworker "github.com/klb3713/ontoaxis/internal/modules/actions/worker"
)

// TestActionWorkflowWithRealTemporalAndPostgres is the env-gated real-instance
// test for task 9.2. It requires a migrated actions schema and a running
// Temporal server. Without both environment variables it skips instead of
// silently passing.
func TestActionWorkflowWithRealTemporalAndPostgres(t *testing.T) {
	if os.Getenv("ONTOAXIS_ACTIONS_INTEGRATION") != "1" {
		t.Skip("set ONTOAXIS_ACTIONS_INTEGRATION=1, ONTOAXIS_TEST_DATABASE_URL and ONTOAXIS_TEST_TEMPORAL_HOST to run")
	}
	databaseURL := os.Getenv("ONTOAXIS_TEST_DATABASE_URL")
	hostPort := os.Getenv("ONTOAXIS_TEST_TEMPORAL_HOST")
	if databaseURL == "" || hostPort == "" {
		t.Fatal("ONTOAXIS_TEST_DATABASE_URL and ONTOAXIS_TEST_TEMPORAL_HOST are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	store := repo.New(pool)

	tenantID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	actionType, err := store.CreateActionType(ctx, tenantID, "it_noop_"+uuid.NewString()[:8], "IT noop", "", nil, map[string]any{
		"execution_definition": map[string]any{
			"steps": []any{map[string]any{"id": "s1", "activity_type": "noop", "read_only": true}},
		},
	}, "active")
	if err != nil {
		t.Fatalf("create action type: %v", err)
	}

	cfg := actiontemporal.Config{
		HostPort:  hostPort,
		Namespace: actiontemporal.DefaultNamespace,
		TaskQueue: "ontoaxis-actions-it",
		Mode:      service.ExecutionModeTemporal,
	}
	adapter, err := actiontemporal.NewAdapter(cfg)
	if err != nil {
		t.Fatalf("temporal adapter: %v", err)
	}
	defer adapter.Close()

	activities := actiontemporal.NewActivities(store, actiontemporal.NoopEffectApplier{})
	worker := actionworker.New(cfg, adapter.Client(), activities)
	runCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() { _ = worker.Run(runCtx) }()

	svc := service.New(store, adapter)
	execution, err := svc.Run(ctx, tenantID, actionType.ID, map[string]any{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current, err := svc.GetExecution(ctx, tenantID, execution.ID)
		if err != nil {
			t.Fatalf("get execution: %v", err)
		}
		if current.Status == "succeeded" {
			return
		}
		if current.Status == "failed" || current.Status == "cancelled" {
			t.Fatalf("execution terminal status = %s, error = %s", current.Status, current.Error)
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("execution %s did not reach succeeded within 30s", execution.ID)
}
