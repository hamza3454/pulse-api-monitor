import type { CheckResult, Monitor, Status } from "./types";

/** The status to show: paused monitors say so; otherwise the latest check decides. */
export function statusOf(monitor: Monitor, latest: CheckResult | undefined): Status {
  if (!monitor.enabled) return "paused";
  if (!latest) return "pending";
  return latest.success ? "up" : "down";
}

/** "5s ago", "3m ago", "2h ago", "4d ago". */
export function timeAgo(iso: string, now: number = Date.now()): string {
  const seconds = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function formatLatency(ms: number | null | undefined): string {
  if (ms == null) return "–";
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`;
}

/** "30s", "5m", "2h": how often a monitor is checked. */
export function formatInterval(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  return `${Math.round(seconds / 3600)}h`;
}

/** Share of results that passed, as a whole percent; null with no data. */
export function uptimePercent(results: CheckResult[]): number | null {
  if (results.length === 0) return null;
  return Math.round((results.filter((r) => r.success).length / results.length) * 100);
}
