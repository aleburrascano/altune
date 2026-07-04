/**
 * downloadStore — the single SSE-fed lifecycle store for in-flight downloads,
 * keyed by track id (realtime audit, Wave 2 / F6-F9).
 *
 * It replaces the two-sources-of-truth split that caused the "Finishing up…
 * never paints, then the row vanishes" bug: membership used to come from the
 * cached `acquisition_status === 'pending'` field while the phase came from a
 * separate stage store, and the `completed` event dropped the item (status →
 * ready) in the same tick it set the phase. Here BOTH membership and phase come
 * from one store, seeded by `started`/`progress` and advanced by `completed`,
 * so the row is not unmounted by a cache status flip. A completed download is
 * held through a brief `finishing → done ✓` terminal sequence so the last phase
 * actually animates even when the tag/store/update steps fire back-to-back.
 */

import { useMemo } from 'react';
import { create } from 'zustand';

// A download's lifecycle phase. The three progress phases plus two terminal
// ones. (`working` from stagePhase is only a display fallback, never stored.)
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

// Terminal-sequence timing: how long "Finishing up…" is guaranteed to paint,
// then how long the "Done ✓" state lingers before the row is removed.
export const FINISHING_DWELL_MS = 500;
export const DONE_HOLD_MS = 1_200;
// A failed download lingers briefly in the dock; the library row keeps showing
// the failure persistently from the cache.
export const FAILED_HOLD_MS = 4_000;

// Ordering for the forward-only guard: a late/out-of-order progress event must
// not regress the displayed phase. done/failed are terminal (highest).
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

// Terminal-sequence timers live outside the store (they're scheduling, not
// state), keyed by track id so a re-acquire cancels a prior sequence.
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
    clearTimers(trackId); // a re-acquire cancels any lingering terminal sequence
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
    // A progress event after a terminal state is stale (a re-acquire arrives via
    // start()); ignore it so a finished row doesn't flicker back to active.
    if (cur && (cur.phase === 'done' || cur.phase === 'failed')) return;
    // Forward-only: never regress the displayed phase on an out-of-order event.
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
    // Force "finishing" first so the last phase paints even when tag/store/
    // update_track fired in milliseconds, then hold "done ✓", then remove.
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
    schedule(
      trackId,
      () => set((s) => setPhaseIfPresent(s, trackId, 'done')),
      FINISHING_DWELL_MS,
    );
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

// setPhaseIfPresent updates an existing entry's phase (or, when create=true,
// seeds a minimal entry if the download was never seen — e.g. a completed/failed
// event with no prior progress because the client connected late).
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

// ---- selectors + imperative API (called from the event router, outside React) ----

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

/** Reactive phase for one track (LibraryRow, DownloadsSheet). */
export function useDownloadPhase(trackId: string): DownloadPhase | undefined {
  return useDownloadStore((s) => s.entries[trackId]?.phase);
}

// (DownloadPhase is a subset of stagePhase's AcquisitionPhase, so phaseLabel and
// ACQUISITION_PHASES accept a DownloadPhase directly — no conversion needed.)

/**
 * The dock's in-flight list: every entry except the ones that have failed (a
 * failure surfaces on the library row, not in the "downloading" dock). Includes
 * the brief terminal `done` tail so it animates before removal. Sorted by id for
 * a stable order across renders.
 */
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

/**
 * The bar's aggregate phase across in-flight items (F9): the least-advanced
 * active phase, so a heterogeneous batch shows the earliest work still
 * happening. Returns 'done' only when every item has finished.
 */
export function aggregatePhase(items: DownloadEntry[]): DownloadPhase | undefined {
  if (items.length === 0) return undefined;
  const active = items.filter((e) => e.phase !== 'done');
  if (active.length === 0) return 'done';
  return active.reduce<DownloadPhase>(
    (min, e) => (PHASE_RANK[e.phase] < PHASE_RANK[min] ? e.phase : min),
    active[0]!.phase,
  );
}
