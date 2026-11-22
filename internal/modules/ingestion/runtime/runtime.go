package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Options configures the ingestion runtime runner. It bundles the dispatcher
// and reconciler options so the application wires a single component.
type Options struct {
	Dispatcher DispatcherOptions
	Reconciler ReconcilerOptions
	Logger     *slog.Logger
	Metrics    Metrics
}

// Runtime owns the dispatcher and reconciler goroutines. The application starts
// one Runtime per ingestion module instance; several instances may run
// concurrently because claims and terminal writes are guarded in storage.
type Runtime struct {
	dispatcher *Dispatcher
	reconciler *Reconciler
	logger     *slog.Logger

	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

// NewRuntime builds the runtime components. It validates that every required
// port is present but does not start any goroutine.
func NewRuntime(store RuntimeStore, connectors ConnectorReader, jobRuntime JobRuntime, verifier SnapshotVerifier, opts Options) (*Runtime, error) {
	logger := opts.Logger
	if logger == nil {
		logger = discardLogger()
	}
	dispatcherOpts := opts.Dispatcher
	dispatcherOpts.Logger = logger
	if dispatcherOpts.Metrics == nil {
		dispatcherOpts.Metrics = opts.Metrics
	}
	dispatcher, err := NewDispatcher(store, connectors, jobRuntime, dispatcherOpts)
	if err != nil {
		return nil, fmt.Errorf("build ingestion dispatcher: %w", err)
	}
	reconcilerOpts := opts.Reconciler
	reconcilerOpts.Logger = logger
	if reconcilerOpts.Metrics == nil {
		reconcilerOpts.Metrics = opts.Metrics
	}
	reconciler, err := NewReconciler(store, connectors, jobRuntime, verifier, reconcilerOpts)
	if err != nil {
		return nil, fmt.Errorf("build ingestion reconciler: %w", err)
	}
	return &Runtime{dispatcher: dispatcher, reconciler: reconciler, logger: logger}, nil
}

// Start launches the dispatcher and reconciler goroutines bound to ctx. It is
// non-blocking and idempotent: repeated calls are no-ops.
func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: runtime start context is nil", ErrInvalidConfig)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	r.started = true

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.dispatcher.Run(runCtx)
	}()
	go func() {
		defer wg.Done()
		r.reconciler.Run(runCtx)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
	r.logger.Info("ingestion runtime started")
	return nil
}

// Stop cancels the runtime goroutines. It is safe to call before Start and
// more than once.
func (r *Runtime) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait blocks until both goroutines return. Calling Wait before Start returns
// immediately.
func (r *Runtime) Wait() {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Run starts the runtime and blocks until ctx is done and both goroutines
// have stopped.
func (r *Runtime) Run(ctx context.Context) error {
	if err := r.Start(ctx); err != nil {
		return err
	}
	r.Wait()
	return nil
}
