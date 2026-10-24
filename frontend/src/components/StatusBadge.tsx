import type { Status } from "../types";

const LABELS: Record<Status, string> = {
  up: "Up",
  down: "Down",
  pending: "Waiting",
  paused: "Paused",
};

export function StatusBadge({ status }: { status: Status }) {
  return (
    <span className={`badge badge-${status}`}>
      <span className="dot" aria-hidden="true" />
      {LABELS[status]}
    </span>
  );
}
