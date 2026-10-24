import type { CheckResult, Monitor } from "../types";
import { formatInterval, formatLatency, statusOf, timeAgo } from "../format";
import { useDeleteMonitor, useSetEnabled } from "../hooks";
import { HistoryBars } from "./HistoryBars";
import { StatusBadge } from "./StatusBadge";

interface Props {
  monitors: Monitor[];
  results: Record<string, CheckResult[]>;
  selectedId: string | null;
  onSelect: (id: string | null) => void;
}

export function MonitorList({ monitors, results, selectedId, onSelect }: Props) {
  const setEnabled = useSetEnabled();
  const remove = useDeleteMonitor();

  if (monitors.length === 0) {
    return (
      <div className="card empty">
        <p>No monitors yet.</p>
        <p className="muted">Add one above and Pulse will start checking it.</p>
      </div>
    );
  }

  return (
    <ul className="list">
      {monitors.map((m) => {
        const recent = results[m.id] ?? [];
        const latest = recent[0];
        const status = statusOf(m, latest);
        return (
          <li key={m.id} className={`card row${selectedId === m.id ? " selected" : ""}`}>
            <button className="row-main" onClick={() => onSelect(selectedId === m.id ? null : m.id)} aria-label={`Show history for ${m.name}`}>
              <StatusBadge status={status} />
              <span className="row-title">
                <strong>{m.name}</strong>
                <span className="muted url">{m.url}</span>
              </span>
              <span className="row-stat">
                <span className="label">Latency</span>
                {formatLatency(latest?.latency_ms)}
              </span>
              <span className="row-stat">
                <span className="label">Last check</span>
                {latest ? timeAgo(latest.checked_at) : "–"}
              </span>
              <span className="row-stat">
                <span className="label">Every</span>
                {formatInterval(m.interval_seconds)}
              </span>
              <HistoryBars results={recent} />
            </button>
            <div className="row-actions">
              <button onClick={() => setEnabled.mutate({ id: m.id, enabled: !m.enabled })} disabled={setEnabled.isPending}>
                {m.enabled ? "Pause" : "Resume"}
              </button>
              <button
                className="danger"
                disabled={remove.isPending}
                onClick={() => {
                  if (window.confirm(`Delete "${m.name}" and all its history?`)) {
                    remove.mutate(m.id);
                    if (selectedId === m.id) onSelect(null);
                  }
                }}
              >
                Delete
              </button>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
