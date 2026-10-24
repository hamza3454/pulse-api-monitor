import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError, api } from "./api";

function mockFetch(impl: (url: string, init?: RequestInit) => Response | Promise<Response>) {
  const fn = vi.fn(async (url: string | URL | Request, init?: RequestInit) => impl(String(url), init));
  vi.stubGlobal("fetch", fn);
  return fn;
}
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

afterEach(() => vi.unstubAllGlobals());

describe("api client", () => {
  it("unwraps the monitor list", async () => {
    mockFetch(() => json({ monitors: [{ id: "a" }] }));
    expect(await api.listMonitors()).toEqual([{ id: "a" }]);
  });

  it("sends JSON when creating", async () => {
    const fetch = mockFetch(() => json({ id: "a" }, 201));
    await api.createMonitor({ name: "n", url: "http://x" });
    const [url, init] = fetch.mock.calls[0]!;
    expect(String(url)).toMatch(/\/api\/v1\/monitors$/);
    expect(init?.method).toBe("POST");
    expect(JSON.parse(String(init?.body))).toEqual({ name: "n", url: "http://x" });
  });

  it("turns the API's error envelope into an ApiError with details", async () => {
    mockFetch(() =>
      json({ error: { code: "validation_failed", message: "Invalid monitor", details: ["url is required"] } }, 422),
    );
    const err = await api.createMonitor({ name: "", url: "" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(422);
    expect((err as ApiError).message).toBe("Invalid monitor");
    expect((err as ApiError).details).toEqual(["url is required"]);
  });

  it("copes with an error that is not JSON", async () => {
    mockFetch(() => new Response("<html>bad gateway</html>", { status: 502 }));
    const err = (await api.listMonitors().catch((e: unknown) => e)) as ApiError;
    expect(err.status).toBe(502);
    expect(err.message).toMatch(/502/);
  });

  it("explains when the API cannot be reached", async () => {
    mockFetch(() => {
      throw new TypeError("Failed to fetch");
    });
    const err = (await api.listMonitors().catch((e: unknown) => e)) as ApiError;
    expect(err.status).toBe(0);
    expect(err.message).toMatch(/Cannot reach the Pulse API/);
  });

  it("handles 204 on delete", async () => {
    mockFetch(() => new Response(null, { status: 204 }));
    await expect(api.deleteMonitor("a")).resolves.toBeUndefined();
  });

  it("asks for the requested number of results", async () => {
    const fetch = mockFetch(() => json({ results: [], next_before: null }));
    await api.listResults("abc", 7);
    expect(String(fetch.mock.calls[0]![0])).toMatch(/\/monitors\/abc\/results\?limit=7$/);
  });
});
