package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pulse/internal/domain"
	"pulse/internal/store/postgres"
)

// newTestStore returns a MonitorStore backed by a throwaway schema in the
// database named by PULSE_TEST_DATABASE_URL. The schema is created from the
// real migration file and dropped when the test ends, so tests are isolated
// from each other and from any development data. Without the variable the
// test is skipped, so `go test ./...` works with no database running.
func newTestStore(t *testing.T) *postgres.MonitorStore {
	t.Helper()

	dsn := os.Getenv("PULSE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PULSE_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(admin.Close)

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("random schema name: %v", err)
	}
	schema := "pulse_test_" + hex.EncodeToString(suffix[:])

	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("drop schema: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to schema: %v", err)
	}
	t.Cleanup(pool.Close)

	migration, err := os.ReadFile("../../../migrations/000001_init.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// The simple query protocol allows a file with many statements.
	_, err = conn.Conn().PgConn().Exec(ctx, string(migration)).ReadAll()
	conn.Release()
	if err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	return postgres.NewMonitorStore(pool)
}

func newMonitor(name string) domain.Monitor {
	return domain.Monitor{
		Name:            name,
		URL:             "https://api.example.com/health",
		Method:          "GET",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		ExpectedStatus:  200,
		Enabled:         true,
	}
}

func TestCreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, newMonitor("example"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !domain.IsValidID(created.ID) {
		t.Errorf("ID = %q, want a UUID", created.ID)
	}
	if created.Name != "example" || created.Method != "GET" || created.IntervalSeconds != 60 || !created.Enabled {
		t.Errorf("unexpected stored monitor: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.NextCheckAt.IsZero() {
		t.Errorf("timestamps not set: %+v", created)
	}
	if time.Since(created.NextCheckAt) > time.Minute {
		t.Errorf("NextCheckAt = %v, want roughly now (due immediately)", created.NextCheckAt)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !sameMonitor(got, created) {
		t.Errorf("Get() = %+v, want %+v", got, created)
	}
}

// sameMonitor compares monitors field by field, using time.Equal so the
// result does not depend on how each timestamp's location is represented.
func sameMonitor(a, b domain.Monitor) bool {
	return a.ID == b.ID && a.Name == b.Name && a.URL == b.URL && a.Method == b.Method &&
		a.IntervalSeconds == b.IntervalSeconds && a.TimeoutSeconds == b.TimeoutSeconds &&
		a.ExpectedStatus == b.ExpectedStatus && a.Enabled == b.Enabled &&
		a.NextCheckAt.Equal(b.NextCheckAt) && a.CreatedAt.Equal(b.CreatedAt) && a.UpdatedAt.Equal(b.UpdatedAt)
}

func TestGetNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"3f2504e0-4f89-41d3-9a0c-0305e82c3301", "not-a-uuid", ""} {
		if _, err := store.Get(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Get(%q) error = %v, want ErrNotFound", id, err)
		}
	}
}

func TestListNewestFirstAndNeverNil(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	empty, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("List() on empty table = %#v, want empty non-nil slice", empty)
	}

	first, err := store.Create(ctx, newMonitor("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Create(ctx, newMonitor("second"))
	if err != nil {
		t.Fatal(err)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("List() order = %v, want [second, first]", []string{list[0].Name, list[1].Name})
	}
}

func TestUpdate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, newMonitor("before"))
	if err != nil {
		t.Fatal(err)
	}

	edit := created
	edit.Name = "after"
	edit.URL = "https://other.example.com"
	edit.Method = "HEAD"
	edit.IntervalSeconds = 30
	edit.TimeoutSeconds = 5
	edit.ExpectedStatus = 204

	updated, err := store.Update(ctx, edit)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "after" || updated.URL != "https://other.example.com" || updated.Method != "HEAD" ||
		updated.IntervalSeconds != 30 || updated.TimeoutSeconds != 5 || updated.ExpectedStatus != 204 {
		t.Errorf("fields not updated: %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("UpdatedAt %v not after %v", updated.UpdatedAt, created.UpdatedAt)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt changed: %v -> %v", created.CreatedAt, updated.CreatedAt)
	}
	if !updated.NextCheckAt.Equal(created.NextCheckAt) {
		t.Errorf("NextCheckAt changed by a plain edit: %v -> %v", created.NextCheckAt, updated.NextCheckAt)
	}
}

func TestUpdatePauseAndResume(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, newMonitor("pausable"))
	if err != nil {
		t.Fatal(err)
	}

	paused := created
	paused.Enabled = false
	pausedGot, err := store.Update(ctx, paused)
	if err != nil {
		t.Fatal(err)
	}
	if pausedGot.Enabled {
		t.Fatal("monitor still enabled after pause")
	}
	if !pausedGot.NextCheckAt.Equal(created.NextCheckAt) {
		t.Errorf("pausing changed NextCheckAt: %v -> %v", created.NextCheckAt, pausedGot.NextCheckAt)
	}

	resumed := pausedGot
	resumed.Enabled = true
	resumedGot, err := store.Update(ctx, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if !resumedGot.Enabled {
		t.Fatal("monitor not enabled after resume")
	}
	if !resumedGot.NextCheckAt.After(created.NextCheckAt) {
		t.Errorf("resuming should make the monitor due now: NextCheckAt %v, was %v", resumedGot.NextCheckAt, created.NextCheckAt)
	}
}

func TestUpdateNotFound(t *testing.T) {
	store := newTestStore(t)
	m := newMonitor("ghost")
	m.ID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	if _, err := store.Update(context.Background(), m); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, newMonitor("doomed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get() after delete error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete() error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete(bad id) error = %v, want ErrNotFound", err)
	}
}
