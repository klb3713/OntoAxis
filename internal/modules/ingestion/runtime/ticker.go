package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// runtimeCleanupTimeout bounds best-effort cleanup calls (release lease, stop an
// orphaned runtime) made after a state transition.
const runtimeCleanupTimeout = 10 * time.Second

// Ticker abstracts the periodic wake-up used by the dispatcher and reconciler.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// TickerFactory builds a Ticker for one interval. Tests inject manual tickers.
type TickerFactory func(interval time.Duration) Ticker

// DefaultTickerFactory returns a real time.Ticker.
func DefaultTickerFactory(interval time.Duration) Ticker {
	return &realTicker{ticker: time.NewTicker(interval)}
}

type realTicker struct {
	ticker *time.Ticker
}

func (t *realTicker) C() <-chan time.Time { return t.ticker.C }
func (t *realTicker) Stop()               { t.ticker.Stop() }

// SleepContext sleeps for d unless ctx is done first.
func SleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// discardLogger drops every record. It avoids nil checks in hot paths.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// defaultLeaseOwner identifies this process and instance as a dispatcher.
func defaultLeaseOwner() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "ingestion-dispatcher"
	}
	return fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.NewString())
}

func clockOrNow(clock func() time.Time) func() time.Time {
	if clock != nil {
		return clock
	}
	return time.Now
}

func randFloat64() float64 { return rand.Float64() }
