package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestRuntime(t *testing.T, store *fakeStore, reader ConnectorReader, jobRuntime JobRuntime, verifier SnapshotVerifier) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(store, reader, jobRuntime, verifier, Options{
		Dispatcher: DispatcherOptions{
			Interval:   time.Hour,
			MaxBatch:   5,
			LeaseOwner: "runner-test",
			Logger:     discardLogger(),
		},
		Reconciler: ReconcilerOptions{
			Interval:  time.Hour,
			BatchSize: 5,
			Logger:    discardLogger(),
		},
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for the ingestion runtime to stop")
	}
}

func TestNewRuntimeRequiresPorts(t *testing.T) {
	t.Parallel()
	store := newFakeStore(newFakeClock())
	reader := newFakeConnectorReader()
	jobRuntime := newFakeJobRuntime()

	if _, err := NewRuntime(nil, reader, jobRuntime, nil, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewRuntime(nil store) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewRuntime(store, nil, jobRuntime, nil, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewRuntime(nil connectors) error = %v, want ErrInvalidConfig", err)
	}
	if _, err := NewRuntime(store, reader, nil, nil, Options{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewRuntime(nil job runtime) error = %v, want ErrInvalidConfig", err)
	}
}

func TestRuntimeStartIsIdempotentAndStops(t *testing.T) {
	t.Parallel()
	runtime := newTestRuntime(t, newFakeStore(newFakeClock()), newFakeConnectorReader(), newFakeJobRuntime(), nil)
	ctx := context.Background()

	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	done := make(chan struct{})
	go func() {
		runtime.Wait()
		close(done)
	}()
	runtime.Stop()
	waitDone(t, done)
}

func TestRuntimeRunReturnsOnContextCancel(t *testing.T) {
	t.Parallel()
	runtime := newTestRuntime(t, newFakeStore(newFakeClock()), newFakeConnectorReader(), newFakeJobRuntime(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- runtime.Run(ctx)
	}()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run() to return")
	}
}

func TestRuntimeStartRejectsNilContext(t *testing.T) {
	t.Parallel()
	runtime := newTestRuntime(t, newFakeStore(newFakeClock()), newFakeConnectorReader(), newFakeJobRuntime(), nil)
	// SA1012 禁止传 nil context；本用例专门验证 Start 拒绝 nil 的防御路径。
	//nolint:staticcheck // 见上：这是被测试的拒绝路径，不是生产调用方式
	if err := runtime.Start(nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Start(nil) error = %v, want ErrInvalidConfig", err)
	}
}

func TestRuntimeStopBeforeStartIsNoop(t *testing.T) {
	t.Parallel()
	runtime := newTestRuntime(t, newFakeStore(newFakeClock()), newFakeConnectorReader(), newFakeJobRuntime(), nil)
	runtime.Stop()
	runtime.Wait()
}
