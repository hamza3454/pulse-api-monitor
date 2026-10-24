import type { Monitor, NewMonitor, ResultsPage } from "./types";

const BASE = (import.meta.env.VITE_API_URL ?? "http://localhost:8080").replace(/\/$/, "");

/** An error answer from the API, or a failure to reach it at all. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number, // 0 = could not reach the API
    readonly details: string[] = [],
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      ...init,
      headers: { "Content-Type": "application/json", ...init?.headers },
    });
  } catch {
    throw new ApiError(`Cannot reach the Pulse API at ${BASE}. Is it running?`, 0);
  }

  if (res.status === 204) return undefined as T;

  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    // The API always answers errors as {"error": {"code", "message", "details"}}.
    const err = (body as { error?: { message?: string; details?: unknown } } | null)?.error;
    const details = Array.isArray(err?.details) ? err.details.map(String) : [];
    throw new ApiError(err?.message ?? `Request failed (${res.status})`, res.status, details);
  }
  return body as T;
}

export const api = {
  async listMonitors(): Promise<Monitor[]> {
    return (await request<{ monitors: Monitor[] }>("/api/v1/monitors")).monitors;
  },
  createMonitor(m: NewMonitor): Promise<Monitor> {
    return request("/api/v1/monitors", { method: "POST", body: JSON.stringify(m) });
  },
  setEnabled(id: string, enabled: boolean): Promise<Monitor> {
    return request(`/api/v1/monitors/${id}`, { method: "PATCH", body: JSON.stringify({ enabled }) });
  },
  deleteMonitor(id: string): Promise<void> {
    return request(`/api/v1/monitors/${id}`, { method: "DELETE" });
  },
  listResults(id: string, limit = 50): Promise<ResultsPage> {
    return request(`/api/v1/monitors/${id}/results?limit=${limit}`);
  },
};
