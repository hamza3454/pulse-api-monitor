package worker

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"pulse/internal/domain"
)

// ErrPoolClosed is returned by Submit once the pool has been shut down.
var ErrPoolClosed = errors.New("worker pool is closed")

const saveTimeout = 5 * time.Second

// CheckRunner performs one check. *Checker implements it; tests use fakes.
type CheckRunner interface {
	Check(ctx context.Context, m domain.Monitor) domain.CheckResult
}

// ResultSink stores finished results. *postgres.ResultStore implements it.
type ResultSink interface {
	Insert(ctx context.Context, r domain.CheckResult) error
}

// Pool runs checks on a fixed number of goroutines fed by a bounded queue.
//
//	Submit ──▶ [ queue (buffered channel) ] ──▶ worker 1 ─┐
//	                                          ├─▶ worker 2 ─┼─▶ check ─▶ save result
//	                                          └─▶ worker N ─┘
//
// Two limits keep it from overwhelming the machine or the targets: at most
// `workers` checks run at once, and at most `queueSize` more wait. When both
// are full, Submit blocks. That is backpressure: the caller (the scheduler)
// slows down instead of work piling up without bound.
type Pool struct {
	checker CheckRunner
	sink    ResultSink
	logger  *slog.Logger

	jobs chan domain.Monitor
	wg   sync.WaitGroup

	mu     sync.RWMutex // guards closed and the close of jobs
	closed bool

	// workCtx is what running checks use. It is only canceled when a shutdown
	// runs out of patience, to interrupt checks that are still in flight.
	workCtx    context.Context
	cancelWork context.CancelFunc
}

// NewPool starts `workers` goroutines (at least 1) with a queue of queueSize
// waiting monitors (0 means hand-off only: Submit waits for an idle worker).
func NewPool(workers, queueSize int, checker CheckRunner, sink ResultSink, logger *slog.Logger) *Pool {
	workers = max(workers, 1)
	queueSize = max(queueSize, 0)
	if logger == nil {
		logger = slog.Default()
	}

	workCtx, cancelWork := context.WithCancel(context.Background())
	p := &Pool{
		checker:    checker,
		sink:       sink,
		logger:     logger,
		jobs:       make(chan domain.Monitor, queueSize),
		workCtx:    workCtx,
		cancelWork: cancelWork,
	}
	p.wg.Add(workers)
	for range workers {
		go p.worker()
	}
	return p
}

// Submit queues a monitor to be checked. It blocks while the queue is full and
// returns ctx.Err() if ctx ends first, or ErrPoolClosed after Shutdown.
//
// Stop all submitters before calling Shutdown; a Submit blocked on a full
// queue delays Shutdown until it returns.
func (p *Pool) Submit(ctx context.Context, m domain.Monitor) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return ErrPoolClosed
	}
	select {
	case p.jobs <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown stops accepting work, lets queued and running checks finish, and
// waits for the workers to exit. If ctx ends first, running checks are
// interrupted (their results are discarded, not recorded as outages) and
// ctx.Err() is returned once the workers have stopped. It is safe to call more
// than once.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.jobs)
	}
	p.mu.Unlock()

	finished := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		p.cancelWork()
		return nil
	case <-ctx.Done():
		p.cancelWork() // interrupt in-flight HTTP requests
		<-finished     // they return promptly once canceled
		return ctx.Err()
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()
	// Reading from a closed channel still yields the queued items first, so
	// Shutdown drains the queue before the workers exit.
	for m := range p.jobs {
		p.process(m)
	}
}

// process checks one monitor and stores the result. A panic inside a check is
// logged and swallowed so one bad target can never kill the whole process.
func (p *Pool) process(m domain.Monitor) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic while checking monitor",
				slog.String("monitor_id", m.ID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()

	result := p.checker.Check(p.workCtx, m)

	// Interrupted by a forced shutdown: the outcome says nothing about the
	// target, so do not record it.
	if p.workCtx.Err() != nil {
		p.logger.Debug("check abandoned during shutdown", slog.String("monitor_id", m.ID))
		return
	}

	// Saving must still work while the pool is draining, so use a fresh
	// timeout instead of anything that shutdown could cancel.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(p.workCtx), saveTimeout)
	defer cancel()

	switch err := p.sink.Insert(saveCtx, result); {
	case err == nil:
		p.logResult(m, result)
	case errors.Is(err, domain.ErrNotFound):
		// The monitor was deleted while its check was running. Nothing to record.
		p.logger.Debug("monitor deleted during check, dropping result", slog.String("monitor_id", m.ID))
	default:
		p.logger.Error("saving check result failed",
			slog.String("monitor_id", m.ID), slog.Any("error", err))
	}
}

func (p *Pool) logResult(m domain.Monitor, r domain.CheckResult) {
	attrs := []any{
		slog.String("monitor_id", m.ID),
		slog.String("name", m.Name),
		slog.Bool("success", r.Success),
	}
	if r.StatusCode != nil {
		attrs = append(attrs, slog.Int("status", *r.StatusCode))
	}
	if r.LatencyMS != nil {
		attrs = append(attrs, slog.Int("latency_ms", *r.LatencyMS))
	}
	if r.Success {
		p.logger.Debug("check ok", attrs...)
		return
	}
	if r.Error != nil {
		attrs = append(attrs, slog.String("error", *r.Error))
	}
	p.logger.Warn("check failed", attrs...)
}
