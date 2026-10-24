import { useState } from "react";
import { AddMonitorForm } from "./components/AddMonitorForm";
import { ErrorNotice } from "./components/ErrorNotice";
import { MonitorDetail } from "./components/MonitorDetail";
import { MonitorList } from "./components/MonitorList";
import { statusOf } from "./format";
import { useMonitors, useRecentResults } from "./hooks";

export function App() {
  const { data, error, isLoading } = useMonitors();
  const monitors = data ?? [];
  const results = useRecentResults(monitors);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const statuses = monitors.map((m) => statusOf(m, results[m.id]?.[0]));
  const up = statuses.filter((s) => s === "up").length;
  const down = statuses.filter((s) => s === "down").length;
  const selected = monitors.find((m) => m.id === selectedId) ?? null;

  return (
    <main>
      <header className="top">
        <h1>
          <span className="logo" aria-hidden="true" /> Pulse
        </h1>
        {data && (
          <p className="counts" aria-label="Summary">
            <span>{monitors.length} monitors</span>
            <span className="ok">{up} up</span>
            <span className={down > 0 ? "fail" : ""}>{down} down</span>
          </p>
        )}
      </header>

      <AddMonitorForm />
      <ErrorNotice error={error} />
      {isLoading && <p className="muted">Loading monitors…</p>}
      {data && <MonitorList monitors={monitors} results={results} selectedId={selectedId} onSelect={setSelectedId} />}
      {selected && <MonitorDetail monitor={selected} onClose={() => setSelectedId(null)} />}
    </main>
  );
}
