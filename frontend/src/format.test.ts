import { describe, expect, it } from "vitest";
import { formatInterval, formatLatency, statusOf, timeAgo, uptimePercent } from "./format";
import type { CheckResult, Monitor } from "./types";

const monitor = (over: Partial<Monitor> = {}): Monitor => ({
  id: "m1", name: "n", url: "http://x", method: "GET", interval_seconds: 30, timeout_seconds: 10,
  expected_status: 200, enabled: true, next_check_at: "", created_at: "", updated_at: "", ...over,
});
const result = (success: boolean, id = 1): CheckResult => ({
  id, checked_at: "2026-01-01T00:00:00Z", success, status_code: success ? 200 : 503,
  latency_ms: 10, error: success ? null : "boom",
});

describe("statusOf", () => {
  it("is paused whenever the monitor is disabled, even with results", () => {
    expect(statusOf(monitor({ enabled: false }), result(true))).toBe("paused");
  });
  it("is pending until the first check lands", () => {
    expect(statusOf(monitor(), undefined)).toBe("pending");
  });
  it("follows the latest result", () => {
    expect(statusOf(monitor(), result(true))).toBe("up");
    expect(statusOf(monitor(), result(false))).toBe("down");
  });
});

describe("timeAgo", () => {
  const now = new Date("2026-01-01T12:00:00Z").getTime();
  const ago = (s: number) => timeAgo(new Date(now - s * 1000).toISOString(), now);
  it("picks a sensible unit", () => {
    expect(ago(5)).toBe("5s ago");
    expect(ago(59)).toBe("59s ago");
    expect(ago(60)).toBe("1m ago");
    expect(ago(3599)).toBe("59m ago");
    expect(ago(3600)).toBe("1h ago");
    expect(ago(86400 * 3)).toBe("3d ago");
  });
  it("never shows negative time when the clocks disagree", () => {
    expect(timeAgo(new Date(now + 5000).toISOString(), now)).toBe("0s ago");
  });
});

describe("formatters", () => {
  it("formats latency", () => {
    expect(formatLatency(null)).toBe("–");
    expect(formatLatency(undefined)).toBe("–");
    expect(formatLatency(0)).toBe("0ms");
    expect(formatLatency(250)).toBe("250ms");
    expect(formatLatency(1500)).toBe("1.5s");
  });
  it("formats intervals", () => {
    expect(formatInterval(10)).toBe("10s");
    expect(formatInterval(60)).toBe("1m");
    expect(formatInterval(300)).toBe("5m");
    expect(formatInterval(7200)).toBe("2h");
  });
  it("computes uptime", () => {
    expect(uptimePercent([])).toBeNull();
    expect(uptimePercent([result(true, 1), result(false, 2), result(true, 3), result(true, 4)])).toBe(75);
  });
});
