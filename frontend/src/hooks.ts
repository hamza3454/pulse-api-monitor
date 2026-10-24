import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import type { CheckResult, Monitor, NewMonitor } from "./types";

/** How often the dashboard re-fetches, so the page updates by itself. */
export const REFRESH_MS = 5000;

export const monitorsKey = ["monitors"] as const;
export const resultsKey = (id: string) => ["results", id] as const;

export function useMonitors() {
  return useQuery({ queryKey: monitorsKey, queryFn: api.listMonitors, refetchInterval: REFRESH_MS });
}

export function useResults(id: string, limit: number) {
  return useQuery({
    queryKey: [...resultsKey(id), limit],
    queryFn: () => api.listResults(id, limit),
    refetchInterval: REFRESH_MS,
  });
}

/** Recent checks shown per monitor on the main list. */
export const LIST_RESULTS = 30;

/**
 * Fetches each monitor's recent checks and returns them keyed by monitor id
 * (newest first). One request per monitor for now; Phase 4 replaces this with
 * a cached "latest status" so the list needs a single call.
 */
export function useRecentResults(monitors: Monitor[]): Record<string, CheckResult[]> {
  const queries = useQueries({
    queries: monitors.map((m) => ({
      queryKey: [...resultsKey(m.id), LIST_RESULTS],
      queryFn: () => api.listResults(m.id, LIST_RESULTS),
      refetchInterval: REFRESH_MS,
    })),
  });
  const out: Record<string, CheckResult[]> = {};
  monitors.forEach((m, i) => {
    out[m.id] = queries[i]?.data?.results ?? [];
  });
  return out;
}

export function useCreateMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (m: NewMonitor) => api.createMonitor(m),
    onSuccess: () => qc.invalidateQueries({ queryKey: monitorsKey }),
  });
}

export function useSetEnabled() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => api.setEnabled(id, enabled),
    onSuccess: () => qc.invalidateQueries({ queryKey: monitorsKey }),
  });
}

export function useDeleteMonitor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteMonitor(id),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: resultsKey(id) });
      return qc.invalidateQueries({ queryKey: monitorsKey });
    },
  });
}
