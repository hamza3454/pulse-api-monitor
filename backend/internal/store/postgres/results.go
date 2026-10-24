package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"pulse/internal/domain"
)

// foreignKeyViolation is PostgreSQL's error code for "the referenced row does
// not exist" (here: the monitor was deleted).
const foreignKeyViolation = "23503"

const (
	// ON CONFLICT (job_id) DO NOTHING is what makes saving a result
	// idempotent: storing the same job twice leaves exactly one row.
	insertResultSQL = `
		INSERT INTO check_results (monitor_id, job_id, checked_at, status_code, latency_ms, success, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (job_id) DO NOTHING`

	// $3 is an optional "before" cursor: pass NULL for the newest results, or
	// the checked_at of the last row you already have to get the next page.
	// Served by the (monitor_id, checked_at DESC) index.
	listResultsSQL = `
		SELECT id, monitor_id::text, job_id::text, checked_at, status_code, latency_ms, success, error
		FROM check_results
		WHERE monitor_id = $1 AND ($3::timestamptz IS NULL OR checked_at < $3)
		ORDER BY checked_at DESC, id DESC
		LIMIT $2`
)

// ResultStore reads and writes check results in PostgreSQL.
type ResultStore struct {
	pool *pgxpool.Pool
}

// NewResultStore returns a store backed by pool.
func NewResultStore(pool *pgxpool.Pool) *ResultStore {
	return &ResultStore{pool: pool}
}

// Insert saves a check result. Saving a result whose JobID is already stored is
// a successful no-op. If the monitor no longer exists (deleted while its check
// was running) it returns domain.ErrNotFound and stores nothing.
func (s *ResultStore) Insert(ctx context.Context, r domain.CheckResult) error {
	_, err := s.pool.Exec(ctx, insertResultSQL,
		r.MonitorID, r.JobID, r.CheckedAt, r.StatusCode, r.LatencyMS, r.Success, r.Error)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
			return domain.ErrNotFound
		}
		return fmt.Errorf("insert check result: %w", err)
	}
	return nil
}

// List returns up to limit results for a monitor, newest first. If before is
// non-nil only results checked strictly before it are returned. It never
// returns a nil slice.
func (s *ResultStore) List(ctx context.Context, monitorID string, limit int, before *time.Time) ([]domain.CheckResult, error) {
	if !domain.IsValidID(monitorID) || limit <= 0 {
		return []domain.CheckResult{}, nil
	}
	rows, err := s.pool.Query(ctx, listResultsSQL, monitorID, limit, before)
	if err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	defer rows.Close()

	results := []domain.CheckResult{}
	for rows.Next() {
		var r domain.CheckResult
		if err := rows.Scan(&r.ID, &r.MonitorID, &r.JobID, &r.CheckedAt,
			&r.StatusCode, &r.LatencyMS, &r.Success, &r.Error); err != nil {
			return nil, fmt.Errorf("scan check result: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list check results: %w", err)
	}
	return results, nil
}
