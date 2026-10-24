// Command api is the Pulse REST API service. In Phase 1 it also runs the
// scheduler and the check workers inside the same process; Phase 2 moves them
// into their own services.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pulse/internal/api"
	"pulse/internal/config"
	"pulse/internal/scheduler"
	"pulse/internal/store/postgres"
	"pulse/internal/worker"
)

// shutdownGrace is how long, in total, the HTTP server and the running checks
// get to finish after a shutdown signal before they are interrupted.
const shutdownGrace = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// ctx is canceled on SIGINT/SIGTERM. It is also what stops the scheduler.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	db, err := postgres.Connect(connectCtx, cfg.DatabaseURL)
	cancelConnect()
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	// Closed last (deferred): the workers still need the database until they
	// have finished and saved their results.
	defer db.Close()
	logger.Info("connected to postgres")

	monitors := postgres.NewMonitorStore(db)
	results := postgres.NewResultStore(db)

	// Start the scheduler and worker pool (unless PULSE_WORKERS=0).
	var pool *worker.Pool
	schedulerDone := make(chan struct{})
	if cfg.Workers > 0 {
		queue := cfg.Workers * 2 // checks allowed to wait while every worker is busy
		pool = worker.NewPool(cfg.Workers, queue, worker.NewChecker(), results, logger)
		sched := scheduler.New(monitors, pool, cfg.SchedulerTick, queue, logger)
		go func() {
			defer close(schedulerDone)
			sched.Run(ctx)
		}()
		logger.Info("monitoring started", "workers", cfg.Workers, "tick", cfg.SchedulerTick.String())
	} else {
		close(schedulerDone)
		logger.Warn("PULSE_WORKERS=0: monitoring is off; the API will store monitors but never check them")
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: api.NewRouter(api.Deps{
			Monitors:   monitors,
			Results:    results,
			DB:         db,
			Logger:     logger,
			CORSOrigin: cfg.CORSOrigin,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		serveErr <- srv.ListenAndServe()
	}()

	var runErr error
	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	// Shut down in dependency order:
	//  1. stop the scheduler, the source of new work,
	//  2. stop accepting HTTP requests and let in-flight ones finish,
	//  3. let checks that are already running finish and save their results.
	// The database connection closes after all of that (see the deferred Close).
	stop()
	<-schedulerDone

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = fmt.Errorf("graceful shutdown: %w", err)
	}
	if pool != nil {
		if err := pool.Shutdown(shutdownCtx); err != nil {
			logger.Warn("running checks did not finish in time and were interrupted", "error", err)
		}
	}

	if runErr == nil {
		logger.Info("api stopped cleanly")
	}
	return runErr
}
