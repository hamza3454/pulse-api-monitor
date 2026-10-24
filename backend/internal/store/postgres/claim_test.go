package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulse/internal/store/postgres"
)

// setNextCheck moves a monitor's schedule directly, so tests can make a
// monitor due (past) or not due (future) without waiting.
func setNextCheck(t *testing.T, pool *pgxpool.Pool, id string, offset time.Duration) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE monitors SET next_check_at = now() + make_interval(secs => $2) WHERE id = $1`,
		id, offset.Seconds())
	if err != nil {
		t.Fatalf("set next_check_at: %v", err)
	}
}

func TestClaimDueOnlyClaimsDueEnabledMonitors(t *testing.T) {
	pool := newTestPool(t)
	store := postgres.NewMonitorStore(pool)
	ctx := context.Background()

	due, _ := store.Create(ctx, newMonitor("due"))
	notDue, _ := store.Create(ctx, newMonitor("not due"))
	paused := newMonitor("paused")
	paused.Enabled = false
	pausedM, _ := store.Create(ctx, paused)

	setNextCheck(t, pool, due.ID, -time.Minute)
	setNextCheck(t, pool, notDue.ID, time.Hour)
	setNextCheck(t, pool, pausedM.ID, -time.Minute) // due, but disabled

	claimed, err := store.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != due.ID {
		t.Fatalf("claimed %d monitors %+v, want only the due, enabled one", len(claimed), claimed)
	}
}

func TestClaimDueReschedulesSoSecondClaimIsEmpty(t *testing.T) {
	pool := newTestPool(t)
	store := postgres.NewMonitorStore(pool)
	ctx := context.Background()

	m := newMonitor("every 60s")
	m.IntervalSeconds = 60
	created, _ := store.Create(ctx, m)
	setNextCheck(t, pool, created.ID, -time.Second)

	first, err := store.ClaimDue(ctx, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("first ClaimDue() = %v, %v; want 1 monitor", first, err)
	}

	// The returned monitor carries its new schedule: about now + 60s.
	wait := time.Until(first[0].NextCheckAt)
	if wait < 55*time.Second || wait > 61*time.Second {
		t.Errorf("rescheduled %v from now, want about 60s", wait)
	}

	second, err := store.ClaimDue(ctx, 10)
	if err != nil {
		t.Fatalf("second ClaimDue() error = %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second ClaimDue() returned %d monitors, want 0 (already claimed)", len(second))
	}
}

func TestClaimDueRespectsLimitAndOldestFirst(t *testing.T) {
	pool := newTestPool(t)
	store := postgres.NewMonitorStore(pool)
	ctx := context.Background()

	var ids []string // oldest due first
	for i := range 5 {
		m, _ := store.Create(ctx, newMonitor(fmt.Sprintf("m%d", i)))
		setNextCheck(t, pool, m.ID, -time.Duration(10-i)*time.Minute)
		ids = append(ids, m.ID)
	}

	claimed, err := store.ClaimDue(ctx, 2)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d, want 2 (the limit)", len(claimed))
	}
	got := map[string]bool{claimed[0].ID: true, claimed[1].ID: true}
	if !got[ids[0]] || !got[ids[1]] {
		t.Errorf("claimed %v, want the two longest-overdue monitors %v", got, ids[:2])
	}

	rest, _ := store.ClaimDue(ctx, 10)
	if len(rest) != 3 {
		t.Errorf("remaining claim returned %d, want 3", len(rest))
	}
}

func TestClaimDueNonPositiveLimit(t *testing.T) {
	store := newTestStore(t)
	for _, limit := range []int{0, -1} {
		claimed, err := store.ClaimDue(context.Background(), limit)
		if err != nil || len(claimed) != 0 {
			t.Errorf("ClaimDue(%d) = %v, %v; want nothing", limit, claimed, err)
		}
	}
}

// TestClaimDueConcurrentClaimersNeverOverlap covers the situation
// FOR UPDATE SKIP LOCKED exists for: several schedulers claiming at the same
// moment must split the work between them without ever both taking the same
// monitor (and without waiting on each other's locks).
func TestClaimDueConcurrentClaimersNeverOverlap(t *testing.T) {
	pool := newTestPool(t)
	store := postgres.NewMonitorStore(pool)
	ctx := context.Background()

	const total = 60
	for i := range total {
		m, err := store.Create(ctx, newMonitor(fmt.Sprintf("m%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		setNextCheck(t, pool, m.ID, -time.Minute)
	}

	var (
		mu      sync.Mutex
		claimed = map[string]int{} // monitor ID -> times claimed
		wg      sync.WaitGroup
	)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				batch, err := store.ClaimDue(ctx, 5)
				if err != nil {
					t.Errorf("ClaimDue() error = %v", err)
					return
				}
				if len(batch) == 0 {
					return
				}
				mu.Lock()
				for _, m := range batch {
					claimed[m.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != total {
		t.Errorf("claimed %d distinct monitors, want all %d", len(claimed), total)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Errorf("monitor %s claimed %d times, want exactly once", id, n)
		}
	}
}
