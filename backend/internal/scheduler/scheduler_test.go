package scheduler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"pulse/internal/domain"
)

type fakeClaimer struct {
	mu    sync.Mutex
	calls int
	// next decides what call number n (1-based) returns.
	next func(n, limit int) ([]domain.Monitor, error)
}

func (f *fakeClaimer) ClaimDue(_ context.Context, limit int) ([]domain.Monitor, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	return f.next(n, limit)
}

func (f *fakeClaimer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeSubmitter struct {
	mu        sync.Mutex
	submitted []string
	submit    func(ctx context.Context, m domain.Monitor) error // optional override
}

func (f *fakeSubmitter) Submit(ctx context.Context, m domain.Monitor) error {
	if f.submit != nil {
		return f.submit(ctx, m)
	}
	f.mu.Lock()
	f.submitted = append(f.submitted, m.ID)
	f.mu.Unlock()
	return nil
}

func (f *fakeSubmitter) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.submitted...)
}

func monitors(ids ...string) []domain.Monitor {
	out := make([]domain.Monitor, len(ids))
	for i, id := range ids {
		out[i] = domain.Monitor{ID: id}
	}
	return out
}

// startScheduler runs s in the background and returns a stop function that
// cancels it and waits for Run to return.
func startScheduler(t *testing.T, s *Scheduler) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after its context was canceled")
		}
	}
}

// eventually polls cond until it is true or the deadline passes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunClaimsImmediatelyAndOnEveryTick(t *testing.T) {
	claimer := &fakeClaimer{next: func(int, int) ([]domain.Monitor, error) { return nil, nil }}
	s := New(claimer, &fakeSubmitter{}, 10*time.Millisecond, 5, quiet())

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "at least 4 claims (one at startup, then one per tick)", func() bool { return claimer.callCount() >= 4 })
}

func TestRunStartsWithoutWaitingForFirstTick(t *testing.T) {
	claimer := &fakeClaimer{next: func(int, int) ([]domain.Monitor, error) { return nil, nil }}
	s := New(claimer, &fakeSubmitter{}, time.Hour, 5, quiet()) // tick far in the future

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "the startup claim", func() bool { return claimer.callCount() == 1 })
}

func TestRunSubmitsClaimedMonitorsInOrder(t *testing.T) {
	claimer := &fakeClaimer{next: func(n, _ int) ([]domain.Monitor, error) {
		if n == 1 {
			return monitors("a", "b", "c"), nil
		}
		return nil, nil
	}}
	sub := &fakeSubmitter{}
	s := New(claimer, sub, time.Hour, 10, quiet())

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "three submissions", func() bool { return len(sub.ids()) == 3 })
	if got := strings.Join(sub.ids(), ","); got != "a,b,c" {
		t.Errorf("submitted %s, want a,b,c", got)
	}
}

func TestRunPassesBatchSizeToClaimer(t *testing.T) {
	var gotLimit int
	var mu sync.Mutex
	claimer := &fakeClaimer{next: func(_, limit int) ([]domain.Monitor, error) {
		mu.Lock()
		gotLimit = limit
		mu.Unlock()
		return nil, nil
	}}
	s := New(claimer, &fakeSubmitter{}, time.Hour, 7, quiet())

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "a claim", func() bool { return claimer.callCount() >= 1 })
	mu.Lock()
	defer mu.Unlock()
	if gotLimit != 7 {
		t.Errorf("claim limit = %d, want the batch size 7", gotLimit)
	}
}

func TestRunDrainsBacklogWithoutWaitingForNextTick(t *testing.T) {
	// batch is 2: two full batches mean more may be due, a short one means done.
	claimer := &fakeClaimer{next: func(n, _ int) ([]domain.Monitor, error) {
		switch n {
		case 1:
			return monitors("a", "b"), nil
		case 2:
			return monitors("c", "d"), nil
		case 3:
			return monitors("e"), nil
		default:
			return nil, nil
		}
	}}
	sub := &fakeSubmitter{}
	s := New(claimer, sub, time.Hour, 2, quiet()) // the next tick is an hour away

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "all five monitors submitted without any tick", func() bool { return len(sub.ids()) == 5 })
	time.Sleep(20 * time.Millisecond)
	if n := claimer.callCount(); n != 3 {
		t.Errorf("claimed %d times, want exactly 3 (it stops after the short batch)", n)
	}
}

func TestRunKeepsGoingAfterClaimErrors(t *testing.T) {
	claimer := &fakeClaimer{next: func(n, _ int) ([]domain.Monitor, error) {
		if n <= 2 {
			return nil, errors.New("database unavailable")
		}
		if n == 3 {
			return monitors("a"), nil
		}
		return nil, nil
	}}
	sub := &fakeSubmitter{}
	s := New(claimer, sub, 5*time.Millisecond, 5, quiet())

	stop := startScheduler(t, s)
	defer stop()

	eventually(t, "a submission after two failed claims", func() bool { return len(sub.ids()) == 1 })
}

func TestRunLogsAnOutageOnceAndTheRecovery(t *testing.T) {
	var logs bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return logs.Write(p)
	}), nil))

	claimer := &fakeClaimer{next: func(n, _ int) ([]domain.Monitor, error) {
		if n <= 6 {
			return nil, errors.New("database unavailable")
		}
		return nil, nil
	}}
	s := New(claimer, &fakeSubmitter{}, 3*time.Millisecond, 5, logger)

	stop := startScheduler(t, s)
	eventually(t, "recovery after six failures", func() bool { return claimer.callCount() >= 9 })
	stop()

	mu.Lock()
	defer mu.Unlock()
	out := logs.String()
	if n := strings.Count(out, "claiming due monitors failed"); n != 1 {
		t.Errorf("outage logged %d times, want once (not once per tick):\n%s", n, out)
	}
	if !strings.Contains(out, "recovered") {
		t.Errorf("recovery not logged:\n%s", out)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRunStopsPromptlyEvenWhenSubmitIsBlocked(t *testing.T) {
	claimer := &fakeClaimer{next: func(n, _ int) ([]domain.Monitor, error) {
		return monitors("a", "b"), nil
	}}
	blocked := make(chan struct{}, 1)
	sub := &fakeSubmitter{submit: func(ctx context.Context, _ domain.Monitor) error {
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-ctx.Done() // a pool that is permanently full
		return ctx.Err()
	}}
	s := New(claimer, sub, time.Hour, 5, quiet())

	stop := startScheduler(t, s)
	<-blocked
	stop() // fails the test if Run does not return
}

func TestRunStopsWhenContextAlreadyCanceled(t *testing.T) {
	claimer := &fakeClaimer{next: func(int, int) ([]domain.Monitor, error) { return nil, nil }}
	s := New(claimer, &fakeSubmitter{}, time.Hour, 5, quiet())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return for an already-canceled context")
	}
	if n := claimer.callCount(); n != 0 {
		t.Errorf("claimed %d times with a canceled context, want 0", n)
	}
}

func TestNewClampsNonsenseSettings(t *testing.T) {
	s := New(&fakeClaimer{}, &fakeSubmitter{}, -time.Second, 0, nil)
	if s.tick <= 0 || s.batch < 1 || s.logger == nil {
		t.Errorf("settings not clamped: tick=%v batch=%d logger=%v", s.tick, s.batch, s.logger)
	}
}
