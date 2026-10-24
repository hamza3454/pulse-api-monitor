// These mirror the JSON the Go API returns (backend/internal/api/dto.go).

export interface Monitor {
  id: string;
  name: string;
  url: string;
  method: "GET" | "HEAD";
  interval_seconds: number;
  timeout_seconds: number;
  expected_status: number;
  enabled: boolean;
  next_check_at: string;
  created_at: string;
  updated_at: string;
}

export interface NewMonitor {
  name: string;
  url: string;
  method?: "GET" | "HEAD";
  interval_seconds?: number;
  timeout_seconds?: number;
  expected_status?: number;
}

export interface CheckResult {
  id: number;
  checked_at: string;
  success: boolean;
  status_code: number | null; // null when nothing came back (timeout, refused)
  latency_ms: number | null;
  error: string | null;
}

export interface ResultsPage {
  results: CheckResult[];
  next_before: string | null;
}

/** What the dashboard shows for a monitor. */
export type Status = "up" | "down" | "pending" | "paused";
