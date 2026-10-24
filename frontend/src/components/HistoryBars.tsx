import type { CheckResult } from "../types";
import { formatLatency } from "../format";

/** A row of small bars, oldest on the left: green passed, red failed. */
export function HistoryBars({ results }: { results: CheckResult[] }) {
  if (results.length === 0) return <span className="muted">No checks yet</span>;
  const oldestFirst = [...results].reverse();
  return (
    <span className="bars" role="img" aria-label={`Last ${results.length} checks`}>
      {oldestFirst.map((r) => (
        <span
          key={r.id}
          className={r.success ? "bar bar-ok" : "bar bar-fail"}
          title={`${new Date(r.checked_at).toLocaleTimeString()} · ${
            r.success ? formatLatency(r.latency_ms) : (r.error ?? "failed")
          }`}
        />
      ))}
    </span>
  );
}
