package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pulse/internal/domain"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeChecker returns a canned success after running an optional hook.
type fakeChecker struct {
	run func(ctx context.Context, m domain.Monitor)
}

func (f fakeChecker) Check(ctx context.Context, m domain.Monitor) domain.CheckResult {
	if f.run != nil {
		f.run(ctx, m)
	}
	return domain.CheckResult{MonitorID: m.ID, JobID: domain.NewID(), CheckedAt: time.Now(), Success: true}
}

// fakeSink records inserted results.
type fakeSink struct {
	mu      sync.Mutex
	results []domain.CheckResult
	err     error
}

func (f *fakeSink) Insert(_ context.Context, r domain.CheckResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.results = append(f.results, r)
	return nil
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.results)
}

func mon(id string) domain.Monitor { return domain.Monitor{ID: id, Name: id} }

func TestPoolProcessesEverySubmittedMonitor(t *testing.T) {
	sink := &fakeSink{}
	p := NewPool(4, 8, fakeChecker{}, sink, quietLogger())

	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		if err := p.Submit(context.Background(), mon(id)); err != nil {
			t.Fatalf("Submit(%s) error = %v", id, err)
		}
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := sink.count(); got != 10 {
		t.Errorf("stored %d results, want 10 (Shutdown must drain the queue)", got)
	}
}

func TestPoolNeverRunsMoreChecksThanWorkers(t *testing.T) {
	const workers = 3
	var running, peak atomic.Int32
	checker := fakeChecker{run: func(context.Context, domain.Monitor) {
		n := running.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
	}}

	sink := &fakeSink{}
	p := NewPool(workers, 5, checker, sink, quietLogger())
	for i := range 30 {
		if err := p.Submit(context.Background(), mon(string(rune('a'+i%26)))); err != nil {
			t.Fatal(err)
		}
	}
	_ = p.Shutdown(context.Background())

	if got := peak.Load(); got > workers {
		t.Errorf("peak concurrency = %d, want at most %d", got, workers)
	}
	if got := peak.Load(); got < 2 {
		t.Errorf("peak concurrency = %d; checks should overlap", got)
	}
	if sink.count() != 30 {
		t.Errorf("stored %d results, want 30", sink.count())
	}
}

func TestSubmitBlocksWhenWorkersAndQueueAreFull(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	checker := fakeChecker{run: func(context.Context, domain.Monitor) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
	}}
	p := NewPool(1, 1, checker, &fakeSink{}, quietLogger())

	ctx := context.Background()
	_ = p.Submit(ctx, mon("running"))
	<-started // the only worker is now busy
	if err := p.Submit(ctx, mon("queued")); err != nil {
		t.Fatalf("second Submit() should fit in the queue, got %v", err)
	}

	// Worker busy and queue full: the next Submit must wait, and give up when
	// its context does.
	waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := p.Submit(waitCtx, mon("overflow"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Submit() error = %v, want context.DeadlineExceeded (backpressure)", err)
	}
	if time.Since(start) < 90*time.Millisecond {
		t.Errorf("Submit returned after %v; it should have waited for its context", time.Since(start))
	}

	close(gate)
	_ = p.Shutdown(ctx)
}

func TestShutdownWaitsForRunningChecksAndSavesTheirResults(t *testing.T) {
	started := make(chan struct{})
	checker := fakeChecker{run: func(context.Context, domain.Monitor) {
		close(started)
		time.Sleep(150 * time.Millisecond)
	}}
	sink := &fakeSink{}
	p := NewPool(1, 0, checker, sink, quietLogger())

	_ = p.Submit(context.Background(), mon("slow"))
	<-started

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if sink.count() != 1 {
		t.Errorf("stored %d results, want 1: a graceful shutdown lets running checks finish", sink.count())
	}
}

func TestForcedShutdownInterruptsChecksAndDiscardsTheirResults(t *testing.T) {
	started := make(chan struct{})
	var sawCancel atomic.Bool
	checker := fakeChecker{run: func(ctx context.Context, _ domain.Monitor) {
		close(started)
		<-ctx.Done() // a check that never finishes on its own
		sawCancel.Store(true)
	}}
	sink := &fakeSink{}
	p := NewPool(1, 0, checker, sink, quietLogger())

	_ = p.Submit(context.Background(), mon("hung"))
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := p.Shutdown(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown() error = %v, want context.DeadlineExceeded", err)
	}
	if !sawCancel.Load() {
		t.Error("the running check's context was never canceled")
	}
	if sink.count() != 0 {
		t.Errorf("stored %d results; an interrupted check must not be recorded as an outage", sink.count())
	}
}

func TestSubmitAfterShutdownFails(t *testing.T) {
	p := NewPool(1, 1, fakeChecker{}, &fakeSink{}, quietLogger())
	_ = p.Shutdown(context.Background())

	if err := p.Submit(context.Background(), mon("late")); !errors.Is(err, ErrPoolClosed) {
		t.Errorf("Submit() after Shutdown error = %v, want ErrPoolClosed", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown() error = %v, want nil (idempotent)", err)
	}
}

func TestPanickingCheckDoesNotKillTheWorker(t *testing.T) {
	var calls atomic.Int32
	checker := fakeChecker{run: func(_ context.Context, m domain.Monitor) {
		calls.Add(1)
		if m.ID == "bad" {
			panic("boom")
		}
	}}
	sink := &fakeSink{}
	p := NewPool(1, 4, checker, sink, quietLogger())

	for _, id := range []string{"bad", "good1", "good2"} {
		_ = p.Submit(context.Background(), mon(id))
	}
	_ = p.Shutdown(context.Background())

	if calls.Load() != 3 {
		t.Errorf("checker ran %d times, want 3", calls.Load())
	}
	if sink.count() != 2 {
		t.Errorf("stored %d results, want 2 (the monitors after the panic must still run)", sink.count())
	}
}

func TestSinkFailuresDoNotStopTheWorker(t *testing.T) {
	var checks atomic.Int32
	checker := fakeChecker{run: func(context.Context, domain.Monitor) { checks.Add(1) }}

	for name, sinkErr := range map[string]error{
		"database error":      errors.New("connection lost"),
		"monitor was deleted": domain.ErrNotFound,
	} {
		checks.Store(0)
		p := NewPool(1, 4, checker, &fakeSink{err: sinkErr}, quietLogger())
		for _, id := range []string{"a", "b", "c"} {
			_ = p.Submit(context.Background(), mon(id))
		}
		if err := p.Shutdown(context.Background()); err != nil {
			t.Errorf("%s: Shutdown() error = %v", name, err)
		}
		if checks.Load() != 3 {
			t.Errorf("%s: ran %d checks, want 3; one failed save must not stop the rest", name, checks.Load())
		}
	}
}

func TestNewPoolClampsNonsenseSizes(t *testing.T) {
	sink := &fakeSink{}
	p := NewPool(0, -5, fakeChecker{}, sink, nil) // 0 workers would deadlock if not clamped
	if err := p.Submit(context.Background(), mon("a")); err != nil {
		t.Fatal(err)
	}
	_ = p.Shutdown(context.Background())
	if sink.count() != 1 {
		t.Errorf("stored %d results, want 1", sink.count())
	}
}
