// Package scheduler decides when monitors are checked. It does no checking
// itself: it claims monitors that are due and hands them to a worker pool.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"pulse/internal/domain"
)

// Claimer finds due monitors and reschedules them atomically.
// *postgres.MonitorStore implements it.
type Claimer interface {
	ClaimDue(ctx context.Context, limit int) ([]domain.Monitor, error)
}

// Submitter accepts monitors to be checked, blocking when it is busy.
// *worker.Pool implements it.
type Submitter interface {
	Submit(ctx context.Context, m domain.Monitor) error
}

// Scheduler repeatedly claims due monitors and submits them for checking.
type Scheduler struct {
	claimer   Claimer
	submitter Submitter
	tick      time.Duration
	batch     int
	logger    *slog.Logger
}

// New returns a Scheduler that looks for due monitors every tick and claims at
// most batch of them at a time.
func New(claimer Claimer, submitter Submitter, tick time.Duration, batch int, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		claimer:   claimer,
		submitter: submitter,
		tick:      max(tick, time.Millisecond),
		batch:     max(batch, 1),
		logger:    logger,
	}
}

// Run blocks until ctx is canceled. It checks for due monitors immediately (so
// a freshly started process does not wait a full tick) and then every tick.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	failing := false // true while the database is erroring; keeps the log quiet
	for {
		s.drain(ctx, &failing)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drain claims and submits batches until a claim comes back smaller than a full
// batch, which means nothing else is due. After an outage or a burst of new
// monitors this clears the backlog right away instead of one batch per tick.
func (s *Scheduler) drain(ctx context.Context, failing *bool) {
	for ctx.Err() == nil {
		monitors, err := s.claimer.ClaimDue(ctx, s.batch)
		if err != nil {
			// Log the first failure only; the next tick retries anyway.
			if ctx.Err() == nil && !*failing {
				s.logger.Error("claiming due monitors failed; will retry", slog.Any("error", err))
			}
			*failing = true
			return
		}
		if *failing {
			s.logger.Info("claiming due monitors recovered")
			*failing = false
		}

		for i, m := range monitors {
			// Submit blocks while the pool is full. That is the backpressure:
			// the scheduler cannot outrun the workers.
			if err := s.submitter.Submit(ctx, m); err != nil {
				// Only happens on shutdown. The monitors not yet submitted were
				// already rescheduled, so each misses one check (see ClaimDue).
				s.logger.Warn("shutting down with claimed monitors not yet submitted",
					slog.Int("dropped", len(monitors)-i), slog.Any("reason", err))
				return
			}
		}

		if len(monitors) < s.batch {
			return
		}
	}
}
