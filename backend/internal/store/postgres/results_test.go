package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"pulse/internal/domain"
	"pulse/internal/store/postgres"
)

func intPtr(n int) *int        { return &n }
func strPtr(s string) *string  { return &s }
func timeAt(sec int) time.Time { return time.Date(2026, 1, 1, 12, 0, sec, 0, time.UTC) }

func successResult(monitorID string, at time.Time) domain.CheckResult {
	return domain.CheckResult{
		MonitorID:  monitorID,
		JobID:      domain.NewID(),
		CheckedAt:  at,
		StatusCode: intPtr(200),
		LatencyMS:  intPtr(42),
		Success:    true,
	}
}

func TestInsertAndListResults(t *testing.T) {
	pool := newTestPool(t)
	monitors := postgres.NewMonitorStore(pool)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	m, _ := monitors.Create(ctx, newMonitor("m"))

	ok := successResult(m.ID, timeAt(1))
	failed := domain.CheckResult{
		MonitorID: m.ID, JobID: domain.NewID(), CheckedAt: timeAt(2),
		Success: false, Error: strPtr("request timed out after 10s"),
		// no status code and no latency: nothing came back
	}
	for _, r := range []domain.CheckResult{ok, failed} {
		if err := results.Insert(ctx, r); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	got, err := results.List(ctx, m.ID, 10, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List() returned %d results, want 2", len(got))
	}

	// Newest first, so the failure (second) comes before the success.
	if got[0].Success || got[0].Error == nil || *got[0].Error != "request timed out after 10s" {
		t.Errorf("first result = %+v, want the failed check", got[0])
	}
	if got[0].StatusCode != nil || got[0].LatencyMS != nil {
		t.Errorf("failed check should have NULL status/latency, got %+v / %+v", got[0].StatusCode, got[0].LatencyMS)
	}
	if !got[1].Success || got[1].StatusCode == nil || *got[1].StatusCode != 200 ||
		got[1].LatencyMS == nil || *got[1].LatencyMS != 42 || got[1].Error != nil {
		t.Errorf("second result = %+v, want the successful check", got[1])
	}
	if got[1].JobID != ok.JobID || got[1].MonitorID != m.ID || got[1].ID == 0 {
		t.Errorf("identifiers not round-tripped: %+v", got[1])
	}
	if !got[1].CheckedAt.Equal(timeAt(1)) {
		t.Errorf("CheckedAt = %v, want %v", got[1].CheckedAt, timeAt(1))
	}
}

func TestInsertSameJobIDTwiceStoresOneRow(t *testing.T) {
	pool := newTestPool(t)
	monitors := postgres.NewMonitorStore(pool)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	m, _ := monitors.Create(ctx, newMonitor("m"))
	r := successResult(m.ID, timeAt(1))

	for i := range 3 {
		if err := results.Insert(ctx, r); err != nil {
			t.Fatalf("Insert() attempt %d error = %v, want nil (duplicate is a no-op)", i+1, err)
		}
	}
	got, _ := results.List(ctx, m.ID, 10, nil)
	if len(got) != 1 {
		t.Errorf("stored %d rows for one job ID, want 1", len(got))
	}
}

func TestInsertForDeletedMonitorIsNotFound(t *testing.T) {
	pool := newTestPool(t)
	results := postgres.NewResultStore(pool)

	r := successResult("3f2504e0-4f89-41d3-9a0c-0305e82c3301", timeAt(1))
	if err := results.Insert(context.Background(), r); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Insert() error = %v, want ErrNotFound", err)
	}
}

func TestListLimitAndBeforeCursor(t *testing.T) {
	pool := newTestPool(t)
	monitors := postgres.NewMonitorStore(pool)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	m, _ := monitors.Create(ctx, newMonitor("m"))
	for sec := 1; sec <= 5; sec++ {
		if err := results.Insert(ctx, successResult(m.ID, timeAt(sec))); err != nil {
			t.Fatal(err)
		}
	}

	page1, _ := results.List(ctx, m.ID, 2, nil)
	if len(page1) != 2 || !page1[0].CheckedAt.Equal(timeAt(5)) || !page1[1].CheckedAt.Equal(timeAt(4)) {
		t.Fatalf("page 1 = %v, want checks at :05 and :04", times(page1))
	}

	cursor := page1[len(page1)-1].CheckedAt
	page2, _ := results.List(ctx, m.ID, 2, &cursor)
	if len(page2) != 2 || !page2[0].CheckedAt.Equal(timeAt(3)) || !page2[1].CheckedAt.Equal(timeAt(2)) {
		t.Fatalf("page 2 = %v, want checks at :03 and :02", times(page2))
	}
}

func TestListOnlyReturnsThisMonitorsResults(t *testing.T) {
	pool := newTestPool(t)
	monitors := postgres.NewMonitorStore(pool)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	a, _ := monitors.Create(ctx, newMonitor("a"))
	b, _ := monitors.Create(ctx, newMonitor("b"))
	_ = results.Insert(ctx, successResult(a.ID, timeAt(1)))
	_ = results.Insert(ctx, successResult(b.ID, timeAt(2)))

	got, _ := results.List(ctx, a.ID, 10, nil)
	if len(got) != 1 || got[0].MonitorID != a.ID {
		t.Errorf("List(a) = %+v, want only a's result", got)
	}
}

func TestListEmptyAndInvalidInputs(t *testing.T) {
	pool := newTestPool(t)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	for name, tc := range map[string]struct {
		id    string
		limit int
	}{
		"no results yet": {"3f2504e0-4f89-41d3-9a0c-0305e82c3301", 10},
		"bad id":         {"not-a-uuid", 10},
		"zero limit":     {"3f2504e0-4f89-41d3-9a0c-0305e82c3301", 0},
	} {
		got, err := results.List(ctx, tc.id, tc.limit, nil)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("%s: List() = %#v, %v; want empty non-nil slice", name, got, err)
		}
	}
}

func TestDeletingMonitorDeletesItsResults(t *testing.T) {
	pool := newTestPool(t)
	monitors := postgres.NewMonitorStore(pool)
	results := postgres.NewResultStore(pool)
	ctx := context.Background()

	m, _ := monitors.Create(ctx, newMonitor("m"))
	_ = results.Insert(ctx, successResult(m.ID, timeAt(1)))
	if err := monitors.Delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM check_results`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d results left after deleting their monitor, want 0 (ON DELETE CASCADE)", n)
	}
}

func times(rs []domain.CheckResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.CheckedAt.Format("04:05")
	}
	return out
}
