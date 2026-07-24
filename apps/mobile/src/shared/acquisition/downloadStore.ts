import { useMemo } from 'react';
import { create } from 'zustand';

export type DownloadPhase = 'finding' | 'downloading' | 'finishing' | 'done' | 'failed';

export interface DownloadEntry {
  trackId: string;
  phase: DownloadPhase;
  title: string | null;
  artist: string | null;
  artworkUrl: string | null;
}

export interface DownloadMeta {
  title?: string | null;
  artist?: string | null;
  artworkUrl?: string | null;
}

export const FINISHING_DWELL_MS = 500;
export const DONE_HOLD_MS = 1_200;
export const FAILED_HOLD_MS = 4_000;

const PHASE_RANK: Record<DownloadPhase, number> = {
  finding: 0,
  downloading: 1,
  finishing: 2,
  done: 3,
  failed: 3,
};

interface DownloadState {
  entries: Record<string, DownloadEntry>;
  start: (trackId: string, meta?: DownloadMeta) => void;
  progress: (trackId: string, phase: DownloadPhase, meta?: DownloadMeta) => void;
  complete: (trackId: string) => void;
  fail: (trackId: string) => void;
  remove: (trackId: string) => void;
  reset: () => void;
}

const timers = new Map<string, ReturnType<typeof setTimeout>[]>();

function clearTimers(trackId: string): void {
  const list = timers.get(trackId);
  if (!list) return;
  list.forEach(clearTimeout);
  timers.delete(trackId);
}

function schedule(trackId: string, fn: () => void, delayMs: number): void {
  const t = setTimeout(fn, delayMs);
  const list = timers.get(trackId) ?? [];
  list.push(t);
  timers.set(trackId, list);
}

function mergeMeta(prev: DownloadEntry | undefined, meta: DownloadMeta | undefined): DownloadMeta {
  return {
    title: meta?.title ?? prev?.title ?? null,
    artist: meta?.artist ?? prev?.artist ?? null,
    artworkUrl: meta?.artworkUrl ?? prev?.artworkUrl ?? null,
  };
}

export const useDownloadStore = create<DownloadState>((set, get) => ({
  entries: {},

  start: (trackId, meta) => {
    clearTimers(trackId);
    set((s) => {
      const merged = mergeMeta(s.entries[trackId], meta);
      return {
        entries: {
          ...s.entries,
          [trackId]: {
            trackId,
            phase: 'finding',
            title: merged.title ?? null,
            artist: merged.artist ?? null,
            artworkUrl: merged.artworkUrl ?? null,
          },
        },
      };
    });
  },

  progress: (trackId, phase, meta) => {
    const cur = get().entries[trackId];
    if (cur && (cur.phase === 'done' || cur.phase === 'failed')) return;
    if (cur && PHASE_RANK[phase] < PHASE_RANK[cur.phase]) return;
    set((s) => {
      const merged = mergeMeta(s.entries[trackId], meta);
      return {
        entries: {
          ...s.entries,
          [trackId]: {
            trackId,
            phase,
            title: merged.title ?? null,
            artist: merged.artist ?? null,
            artworkUrl: merged.artworkUrl ?? null,
          },
        },
      };
    });
  },

  complete: (trackId) => {
    clearTimers(trackId);
    set((s) => {
      const cur = s.entries[trackId];
      const merged = mergeMeta(cur, undefined);
      return {
        entries: {
          ...s.entries,
          [trackId]: {
            trackId,
            phase: 'finishing',
            title: merged.title ?? null,
            artist: merged.artist ?? null,
            artworkUrl: merged.artworkUrl ?? null,
          },
        },
      };
    });
    schedule(trackId, () => set((s) => setPhaseIfPresent(s, trackId, 'done')), FINISHING_DWELL_MS);
    schedule(trackId, () => get().remove(trackId), FINISHING_DWELL_MS + DONE_HOLD_MS);
  },

  fail: (trackId) => {
    clearTimers(trackId);
    set((s) => setPhaseIfPresent(s, trackId, 'failed', true));
    schedule(trackId, () => get().remove(trackId), FAILED_HOLD_MS);
  },

  remove: (trackId) => {
    clearTimers(trackId);
    set((s) => {
      if (!(trackId in s.entries)) return s;
      const next = { ...s.entries };
      delete next[trackId];
      return { entries: next };
    });
  },

  reset: () => {
    timers.forEach((list) => list.forEach(clearTimeout));
    timers.clear();
    set({ entries: {} });
  },
}));

function setPhaseIfPresent(
  s: DownloadState,
  trackId: string,
  phase: DownloadPhase,
  create = false,
): Partial<DownloadState> {
  const cur = s.entries[trackId];
  if (!cur && !create) return s;
  return {
    entries: {
      ...s.entries,
      [trackId]: {
        trackId,
        phase,
        title: cur?.title ?? null,
        artist: cur?.artist ?? null,
        artworkUrl: cur?.artworkUrl ?? null,
      },
    },
  };
}

export function startDownload(trackId: string, meta?: DownloadMeta): void {
  useDownloadStore.getState().start(trackId, meta);
}

export function progressDownload(trackId: string, phase: DownloadPhase, meta?: DownloadMeta): void {
  useDownloadStore.getState().progress(trackId, phase, meta);
}

export function completeDownload(trackId: string): void {
  useDownloadStore.getState().complete(trackId);
}

export function failDownload(trackId: string): void {
  useDownloadStore.getState().fail(trackId);
}

export function useDownloadPhase(trackId: string): DownloadPhase | undefined {
  return useDownloadStore((s) => s.entries[trackId]?.phase);
}

export function useActiveDownloadItems(): DownloadEntry[] {
  const entries = useDownloadStore((s) => s.entries);
  return useMemo(
    () =>
      Object.values(entries)
        .filter((e) => e.phase !== 'failed')
        .sort((a, b) => a.trackId.localeCompare(b.trackId)),
    [entries],
  );
}

export function aggregatePhase(items: DownloadEntry[]): DownloadPhase | undefined {
  if (items.length === 0) return undefined;
  const active = items.filter((e) => e.phase !== 'done');
  if (active.length === 0) return 'done';
  return active.reduce<DownloadPhase>(
    (min, e) => (PHASE_RANK[e.phase] < PHASE_RANK[min] ? e.phase : min),
    active[0]!.phase,
  );
}
