import { useState, type FormEvent } from "react";
import { useCreateMonitor } from "../hooks";
import { ErrorNotice } from "./ErrorNotice";

const INTERVALS = [
  { seconds: 10, label: "Every 10 seconds" },
  { seconds: 30, label: "Every 30 seconds" },
  { seconds: 60, label: "Every minute" },
  { seconds: 300, label: "Every 5 minutes" },
];

export function AddMonitorForm() {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [interval, setInterval] = useState(60);
  const create = useCreateMonitor();

  function submit(e: FormEvent) {
    e.preventDefault();
    create.mutate(
      { name: name.trim(), url: url.trim(), interval_seconds: interval },
      {
        onSuccess: () => {
          setName("");
          setUrl("");
        },
      },
    );
  }

  return (
    <form className="card form" onSubmit={submit}>
      <h2>Add a monitor</h2>
      <div className="form-row">
        <label>
          Name
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Payments API" required maxLength={100} />
        </label>
        <label className="grow">
          URL
          <input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://example.com/health"
            required
            type="url"
          />
        </label>
        <label>
          Check
          <select value={interval} onChange={(e) => setInterval(Number(e.target.value))}>
            {INTERVALS.map((i) => (
              <option key={i.seconds} value={i.seconds}>
                {i.label}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className="primary" disabled={create.isPending}>
          {create.isPending ? "Adding…" : "Add"}
        </button>
      </div>
      <ErrorNotice error={create.error} />
    </form>
  );
}
