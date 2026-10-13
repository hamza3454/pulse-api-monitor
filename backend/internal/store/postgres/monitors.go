package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pulse/internal/domain"
)

// monitorColumns is the column list shared by every query that returns a
// monitor, in the order scanMonitor reads them. The id is cast to text so it
// scans straight into a Go string.
const monitorColumns = `id::text, name, url, method, interval_seconds, timeout_seconds,
	expected_status, enabled, next_check_at, created_at, updated_at`

const (
	insertMonitorSQL = `
		INSERT INTO monitors (name, url, method, interval_seconds, timeout_seconds, expected_status, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + monitorColumns

	getMonitorSQL = `SELECT ` + monitorColumns + ` FROM monitors WHERE id = $1`

	listMonitorsSQL = `SELECT ` + monitorColumns + ` FROM monitors ORDER BY created_at DESC, id`

	// Enabling a paused monitor makes it due immediately; all other updates
	// leave the schedule alone. In an UPDATE, "enabled" on the right-hand side
	// is the row's old value, which is what the CASE needs.
	updateMonitorSQL = `
		UPDATE monitors SET
			name = $2,
			url = $3,
			method = $4,
			interval_seconds = $5,
			timeout_seconds = $6,
			expected_status = $7,
			next_check_at = CASE WHEN $8 AND NOT enabled THEN now() ELSE next_check_at END,
			enabled = $8,
			updated_at = now()
		WHERE id = $1
		RETURNING ` + monitorColumns

	deleteMonitorSQL = `DELETE FROM monitors WHERE id = $1`
)

// MonitorStore reads and writes monitors in PostgreSQL.
type MonitorStore struct {
	pool *pgxpool.Pool
}

// NewMonitorStore returns a store backed by pool.
func NewMonitorStore(pool *pgxpool.Pool) *MonitorStore {
	return &MonitorStore{pool: pool}
}

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanMonitor(row scanner) (domain.Monitor, error) {
	var m domain.Monitor
	err := row.Scan(
		&m.ID, &m.Name, &m.URL, &m.Method, &m.IntervalSeconds, &m.TimeoutSeconds,
		&m.ExpectedStatus, &m.Enabled, &m.NextCheckAt, &m.CreatedAt, &m.UpdatedAt,
	)
	return m, err
}

// Create inserts a monitor and returns it with its generated ID and
// timestamps. It is due for its first check immediately.
func (s *MonitorStore) Create(ctx context.Context, m domain.Monitor) (domain.Monitor, error) {
	created, err := scanMonitor(s.pool.QueryRow(ctx, insertMonitorSQL,
		m.Name, m.URL, m.Method, m.IntervalSeconds, m.TimeoutSeconds, m.ExpectedStatus, m.Enabled))
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("insert monitor: %w", err)
	}
	return created, nil
}

// Get returns the monitor with the given ID, or domain.ErrNotFound.
func (s *MonitorStore) Get(ctx context.Context, id string) (domain.Monitor, error) {
	if !domain.IsValidID(id) {
		return domain.Monitor{}, domain.ErrNotFound
	}
	m, err := scanMonitor(s.pool.QueryRow(ctx, getMonitorSQL, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Monitor{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("get monitor: %w", err)
	}
	return m, nil
}

// List returns all monitors, newest first. It never returns a nil slice.
func (s *MonitorStore) List(ctx context.Context) ([]domain.Monitor, error) {
	rows, err := s.pool.Query(ctx, listMonitorsSQL)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	monitors := []domain.Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitor: %w", err)
		}
		monitors = append(monitors, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	return monitors, nil
}

// Update overwrites the user-editable fields of the monitor with m.ID and
// returns the stored result, or domain.ErrNotFound.
func (s *MonitorStore) Update(ctx context.Context, m domain.Monitor) (domain.Monitor, error) {
	if !domain.IsValidID(m.ID) {
		return domain.Monitor{}, domain.ErrNotFound
	}
	updated, err := scanMonitor(s.pool.QueryRow(ctx, updateMonitorSQL,
		m.ID, m.Name, m.URL, m.Method, m.IntervalSeconds, m.TimeoutSeconds, m.ExpectedStatus, m.Enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Monitor{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	return updated, nil
}

// Delete removes a monitor and, via ON DELETE CASCADE, its check results.
// It returns domain.ErrNotFound if the monitor does not exist.
func (s *MonitorStore) Delete(ctx context.Context, id string) error {
	if !domain.IsValidID(id) {
		return domain.ErrNotFound
	}
	tag, err := s.pool.Exec(ctx, deleteMonitorSQL, id)
	if err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
