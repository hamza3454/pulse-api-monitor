import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import type { CheckResult, Monitor } from "./types";

// A tiny in-memory stand-in for the Pulse API, so the whole UI runs for real.
let monitors: Monitor[];
let results: Record<string, CheckResult[]>;
let nextId: number;

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function fakeApi(url: string, init?: RequestInit): Response {
  const path = new URL(url).pathname;
  const method = init?.method ?? "GET";
  if (path === "/api/v1/monitors" && method === "GET") return json({ monitors });
  if (path === "/api/v1/monitors" && method === "POST") {
    const body = JSON.parse(String(init?.body));
    if (!body.name || !/^https?:\/\//.test(body.url)) {
      return json({ error: { code: "validation_failed", message: "Invalid monitor", details: ["url must start with http"] } }, 422);
    }
    const m = makeMonitor({ id: `m${nextId++}`, ...body });
    monitors = [m, ...monitors];
    return json(m, 201);
  }
  const results_ = path.match(/^\/api\/v1\/monitors\/([^/]+)\/results$/);
  if (results_) return json({ results: results[results_[1]!] ?? [], next_before: null });
  const one = path.match(/^\/api\/v1\/monitors\/([^/]+)$/);
  if (one && method === "PATCH") {
    const patch = JSON.parse(String(init?.body));
    monitors = monitors.map((m) => (m.id === one[1] ? { ...m, ...patch } : m));
    return json(monitors.find((m) => m.id === one[1]));
  }
  if (one && method === "DELETE") {
    monitors = monitors.filter((m) => m.id !== one[1]);
    return new Response(null, { status: 204 });
  }
  return json({ error: { code: "not_found", message: "not found" } }, 404);
}

const makeMonitor = (over: Partial<Monitor>): Monitor => ({
  id: "m0", name: "Mon", url: "http://example.com", method: "GET", interval_seconds: 30, timeout_seconds: 10,
  expected_status: 200, enabled: true, next_check_at: "", created_at: "", updated_at: "", ...over,
});
const check = (id: number, success: boolean, over: Partial<CheckResult> = {}): CheckResult => ({
  id, checked_at: new Date().toISOString(), success, status_code: success ? 200 : 503,
  latency_ms: success ? 42 : 7, error: success ? null : "expected status 200, got 503", ...over,
});

function renderApp() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  monitors = [];
  results = {};
  nextId = 100;
  vi.stubGlobal("fetch", vi.fn(async (url: string | URL | Request, init?: RequestInit) => fakeApi(String(url), init)));
});
afterEach(() => vi.unstubAllGlobals());

describe("dashboard", () => {
  it("shows an empty state", async () => {
    renderApp();
    expect(await screen.findByText("No monitors yet.")).toBeInTheDocument();
  });

  it("shows status, latency and the summary for each monitor", async () => {
    monitors = [
      makeMonitor({ id: "a", name: "Healthy API" }),
      makeMonitor({ id: "b", name: "Broken API" }),
      makeMonitor({ id: "c", name: "Brand new" }),
      makeMonitor({ id: "d", name: "Sleeping", enabled: false }),
    ];
    results = { a: [check(2, true), check(1, true)], b: [check(4, false), check(3, true)], c: [], d: [check(5, true)] };
    renderApp();

    const row = async (name: string) => (await screen.findByText(name)).closest("li")!;
    expect(within(await row("Healthy API")).getByText("Up")).toBeInTheDocument();
    expect(within(await row("Healthy API")).getByText("42ms")).toBeInTheDocument();
    expect(within(await row("Broken API")).getByText("Down")).toBeInTheDocument();
    expect(within(await row("Brand new")).getByText("Waiting")).toBeInTheDocument();
    expect(within(await row("Sleeping")).getByText("Paused")).toBeInTheDocument();

    const summary = screen.getByLabelText("Summary");
    await waitFor(() => expect(summary).toHaveTextContent("4 monitors"));
    expect(summary).toHaveTextContent("1 up");
    expect(summary).toHaveTextContent("1 down");
  });

  it("adds a monitor from the form and clears the inputs", async () => {
    const user = userEvent.setup();
    renderApp();
    await screen.findByText("No monitors yet.");

    await user.type(screen.getByLabelText("Name"), "Payments");
    await user.type(screen.getByLabelText("URL"), "https://pay.example.com/health");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect(await screen.findByText("Payments")).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveValue("");
    expect(screen.getByLabelText("URL")).toHaveValue("");
  });

  it("shows the API's validation messages", async () => {
    const user = userEvent.setup();
    renderApp();
    await screen.findByText("No monitors yet.");
    // type="url" fields are checked by the browser first; bypass that to reach the server's check.
    const form = screen.getByRole("button", { name: "Add" }).closest("form")!;
    form.noValidate = true;
    await user.type(screen.getByLabelText("Name"), "Bad");
    await user.type(screen.getByLabelText("URL"), "ftp://nope");
    await user.click(screen.getByRole("button", { name: "Add" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Invalid monitor");
    expect(alert).toHaveTextContent("url must start with http");
  });

  it("pauses and resumes a monitor", async () => {
    const user = userEvent.setup();
    monitors = [makeMonitor({ id: "a", name: "Toggle me" })];
    results = { a: [check(1, true)] };
    renderApp();

    const li = (await screen.findByText("Toggle me")).closest("li")!;
    await user.click(within(li).getByRole("button", { name: "Pause" }));
    expect(await within(li).findByText("Paused")).toBeInTheDocument();
    await user.click(within(li).getByRole("button", { name: "Resume" }));
    expect(await within(li).findByText("Up")).toBeInTheDocument();
  });

  it("deletes a monitor only after confirmation", async () => {
    const user = userEvent.setup();
    monitors = [makeMonitor({ id: "a", name: "Doomed" })];
    const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    renderApp();

    const li = (await screen.findByText("Doomed")).closest("li")!;
    await user.click(within(li).getByRole("button", { name: "Delete" }));
    expect(screen.getByText("Doomed")).toBeInTheDocument(); // declined: still there

    await user.click(within(li).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(screen.queryByText("Doomed")).not.toBeInTheDocument());
    expect(confirm).toHaveBeenCalledTimes(2);
  });

  it("opens a monitor's history, including failure reasons", async () => {
    const user = userEvent.setup();
    monitors = [makeMonitor({ id: "a", name: "Detailed" })];
    results = { a: [check(2, false), check(1, true)] };
    renderApp();

    await user.click(await screen.findByRole("button", { name: "Show history for Detailed" }));
    const detail = await screen.findByRole("region", { name: "History for Detailed" });
    await waitFor(() => expect(detail).toHaveTextContent("50% of the last 2 checks passed"));
    expect(within(detail).getByText("expected status 200, got 503")).toBeInTheDocument();
    expect(within(detail).getByText("Failed")).toBeInTheDocument();
    expect(within(detail).getByText("Passed")).toBeInTheDocument();

    await user.click(within(detail).getByRole("button", { name: "Close" }));
    expect(screen.queryByRole("region", { name: "History for Detailed" })).not.toBeInTheDocument();
  });

  it("tells the user when the API is unreachable", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new TypeError("Failed to fetch"); }));
    renderApp();
    expect(await screen.findByRole("alert")).toHaveTextContent(/Cannot reach the Pulse API/);
  });
});
