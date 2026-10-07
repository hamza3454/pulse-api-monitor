-- Pulse initial schema: monitors and their raw check results.
-- CHECK constraints mirror the limits in internal/domain/monitor.go.

CREATE TABLE monitors (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    url              TEXT        NOT NULL CHECK (length(url) <= 2048),
    method           TEXT        NOT NULL DEFAULT 'GET' CHECK (method IN ('GET', 'HEAD')),
    interval_seconds INTEGER     NOT NULL DEFAULT 60  CHECK (interval_seconds BETWEEN 10 AND 86400),
    timeout_seconds  INTEGER     NOT NULL DEFAULT 10  CHECK (timeout_seconds BETWEEN 1 AND 60),
    expected_status  INTEGER     NOT NULL DEFAULT 200 CHECK (expected_status BETWEEN 100 AND 599),
    enabled          BOOLEAN     NOT NULL DEFAULT TRUE,
    -- The scheduler (Phase 1+) picks monitors whose next_check_at has passed.
    next_check_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (timeout_seconds <= interval_seconds)
);

-- Partial index: only enabled monitors are ever scheduled, so paused ones
-- cost nothing in the scheduler's "what is due?" query.
CREATE INDEX monitors_due_idx ON monitors (next_check_at) WHERE enabled;

CREATE TABLE check_results (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    monitor_id  UUID        NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    -- Unique per check job. Kafka delivers at-least-once (Phase 2), so a
    -- duplicate job must not create a duplicate row: INSERT ... ON CONFLICT (job_id) DO NOTHING.
    job_id      UUID        NOT NULL UNIQUE,
    checked_at  TIMESTAMPTZ NOT NULL,
    status_code INTEGER              CHECK (status_code BETWEEN 100 AND 599), -- NULL when no response (timeout, DNS, ...)
    latency_ms  INTEGER              CHECK (latency_ms >= 0),
    success     BOOLEAN     NOT NULL,
    error       TEXT                                                         -- NULL on success
);

-- Serves "latest N results for a monitor" and "results in a time range".
CREATE INDEX check_results_monitor_time_idx ON check_results (monitor_id, checked_at DESC);
