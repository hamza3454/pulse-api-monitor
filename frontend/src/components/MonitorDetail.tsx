import type { Monitor } from "../types";
import { formatLatency, timeAgo, uptimePercent } from "../format";
import { useResults } from "../hooks";
import { ErrorNotice } from "./ErrorNotice";

export function MonitorDetail({ monitor, onClose }: { monitor: Monitor; onClose: () => void }) {
  const { data, error, isLoading } = useResults(monitor.id, 50);
  const results = data?.results ?? [];
  const uptime = uptimePercent(results);

  return (
    <section className="card detail" aria-label={`History for ${monitor.name}`}>
      <div className="detail-head">
        <div>
          <h2>{monitor.name}</h2>
          <p className="muted">
            {monitor.method} {monitor.url} · expects {monitor.expected_status} · {monitor.timeout_seconds}s timeout
          </p>
        </div>
        <button onClick={onClose}>Close</button>
      </div>

      <ErrorNotice error={error} />
      {isLoading && <p className="muted">Loading…</p>}
      {data && results.length === 0 && <p className="muted">No checks have run yet.</p>}

      {results.length > 0 && (
        <>
          <p className="summary-line">
            <strong>{uptime}%</strong> of the last {results.length} checks passed
          </p>
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Result</th>
                <th>Status</th>
                <th>Latency</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {results.map((r) => (
                <tr key={r.id}>
                  <td title={new Date(r.checked_at).toLocaleString()}>{timeAgo(r.checked_at)}</td>
                  <td className={r.success ? "ok" : "fail"}>{r.success ? "Passed" : "Failed"}</td>
                  <td>{r.status_code ?? "–"}</td>
                  <td>{formatLatency(r.latency_ms)}</td>
                  <td>{r.error ?? ""}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </section>
  );
}
