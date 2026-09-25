import { useMemo } from 'react';
import { create } from 'zustand';

import type { AcquisitionPhase } from '@shared/acquisition/stagePhase';
import type { TrackId } from '@shared/api-client/ids';
import { onSignOut } from '@shared/session/signOutCleanup';

// Every acquisition phase except the stage-less 'working' fallback, which the
// downloads bar never shows. Derived so a new AcquisitionPhase lands here too.
export type DownloadPhase = Exclude<AcquisitionPhase, 'working'>;

export interface DownloadEntry {
  trackId: TrackId;
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
  remembered: Record<string, DownloadMeta>;
  rememberMeta: (trackId: TrackId, meta: DownloadMeta) => void;
  start: (trackId: TrackId, meta?: DownloadMeta) => void;
  progress: (trackId: TrackId, phase: DownloadPhase, meta?: DownloadMeta) => void;
  complete: (trackId: TrackId) => void;
  fail: (trackId: TrackId) => void;
  remove: (trackId: TrackId) => void;
  reset: () => void;
}

const timers = new Map<TrackId, ReturnType<typeof setTimeout>[]>();

function clearTimers(trackId: TrackId): void {
  const list = timers.get(trackId);
  if (!list) return;
  list.forEach(clearTimeout);
  timers.delete(trackId);
}

function schedule(trackId: TrackId, fn: () => void, delayMs: number): void {
  const t = setTimeout(fn, delayMs);
  const list = timers.get(trackId) ?? [];
  list.push(t);
  timers.set(trackId, list);
}

function pickField(
  sources: readonly (DownloadEntry | DownloadMeta | undefined)[],
  key: 'title' | 'artist' | 'artworkUrl',
): string | null {
  for (const source of sources) {
    const value = source?.[key];
    if (value != null) return value;
  }
  return null;
}

function mergeMeta(sources: readonly (DownloadEntry | DownloadMeta | undefined)[]): DownloadMeta {
  return {
    title: pickField(sources, 'title'),
    artist: pickField(sources, 'artist'),
    artworkUrl: pickField(sources, 'artworkUrl'),
  };
}

function makeEntry(
  trackId: TrackId,
  phase: DownloadPhase,
  prev: DownloadEntry | undefined,
  meta?: DownloadMeta,
  remembered?: DownloadMeta,
): DownloadEntry {
  const merged = mergeMeta([meta, prev, remembered]);
  return {
    trackId,
    phase,
    title: merged.title ?? null,
    artist: merged.artist ?? null,
    artworkUrl: merged.artworkUrl ?? null,
  };
}

function withoutKey<T>(map: Record<string, T>, key: string): Record<string, T> {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

function isStalePhase(cur: DownloadEntry | undefined, phase: DownloadPhase): boolean {
  if (!cur) return false;
  if (cur.phase === 'done' || cur.phase === 'failed') return true;
  return PHASE_RANK[phase] < PHASE_RANK[cur.phase];
}

export const useDownloadStore = create<DownloadState>((set, get) => ({
  entries: {},
  remembered: {},

  rememberMeta: (trackId, meta) => {
    set((s) => ({ remembered: { ...s.remembered, [trackId]: meta } }));
  },

  start: (trackId, meta) => {
    clearTimers(trackId);
    set((s) => ({
      entries: {
        ...s.entries,
        [trackId]: makeEntry(trackId, 'finding', s.entries[trackId], meta, s.remembered[trackId]),
      },
      remembered: withoutKey(s.remembered, trackId),
    }));
  },

  progress: (trackId, phase, meta) => {
    if (isStalePhase(get().entries[trackId], phase)) return;
    set((s) => ({
      entries: {
        ...s.entries,
        [trackId]: makeEntry(trackId, phase, s.entries[trackId], meta, s.remembered[trackId]),
      },
      remembered: withoutKey(s.remembered, trackId),
    }));
  },

  complete: (trackId) => {
    clearTimers(trackId);
    set((s) => ({
      entries: { ...s.entries, [trackId]: makeEntry(trackId, 'finishing', s.entries[trackId]) },
    }));
    schedule(
      trackId,
      () => set((s) => updatePhaseIfPresent(s, trackId, 'done')),
      FINISHING_DWELL_MS,
    );
    schedule(trackId, () => get().remove(trackId), FINISHING_DWELL_MS + DONE_HOLD_MS);
  },

  fail: (trackId) => {
    clearTimers(trackId);
    set((s) => ({
      ...forceSetPhase(s, trackId, 'failed'),
      remembered: withoutKey(s.remembered, trackId),
    }));
    const removeOnceSettled = (): void => {
      // Hold the failure while the rest of its batch is still in flight, so
      // the bar can still count it when the batch lands instead of "Done".
      timers.delete(trackId);
      if (Object.values(get().entries).some(isInFlight)) {
        schedule(trackId, removeOnceSettled, FAILED_HOLD_MS);
      } else {
        get().remove(trackId);
      }
    };
    schedule(trackId, removeOnceSettled, FAILED_HOLD_MS);
  },

  remove: (trackId) => {
    clearTimers(trackId);
    set((s) => {
      if (!(trackId in s.entries) && !(trackId in s.remembered)) return s;
      const next = { ...s.entries };
      delete next[trackId];
      return { entries: next, remembered: withoutKey(s.remembered, trackId) };
    });
  },

  reset: () => {
    timers.forEach((list) => list.forEach(clearTimeout));
    timers.clear();
    set({ entries: {}, remembered: {} });
  },
}));

// The downloads bar and the timers still holding its entries belong to the
// account that started them, so the next identity inherits neither.
onSignOut(() => useDownloadStore.getState().reset());

/** Moves an existing entry to `phase`; a no-op when the track has no entry. */
function updatePhaseIfPresent(
  s: DownloadState,
  trackId: TrackId,
  phase: DownloadPhase,
): Partial<DownloadState> {
  if (!s.entries[trackId]) return s;
  return withPhase(s, trackId, phase);
}

/** Sets `phase` on the track's entry, creating the entry if it does not exist. */
function forceSetPhase(
  s: DownloadState,
  trackId: TrackId,
  phase: DownloadPhase,
): Partial<DownloadState> {
  return withPhase(s, trackId, phase);
}

function withPhase(
  s: DownloadState,
  trackId: TrackId,
  phase: DownloadPhase,
): Partial<DownloadState> {
  return {
    entries: {
      ...s.entries,
      [trackId]: makeEntry(trackId, phase, s.entries[trackId], undefined, s.remembered[trackId]),
    },
  };
}

export function startDownload(trackId: TrackId, meta?: DownloadMeta): void {
  useDownloadStore.getState().start(trackId, meta);
}

export function rememberDownloadMeta(trackId: TrackId, meta: DownloadMeta): void {
  useDownloadStore.getState().rememberMeta(trackId, meta);
}

/** True when `phase` sits behind the phase this track's download has already reached. */
export function isStaleDownloadPhase(trackId: TrackId, phase: DownloadPhase): boolean {
  return isStalePhase(useDownloadStore.getState().entries[trackId], phase);
}

export function progressDownload(
  trackId: TrackId,
  phase: DownloadPhase,
  meta?: DownloadMeta,
): void {
  useDownloadStore.getState().progress(trackId, phase, meta);
}

export function completeDownload(trackId: TrackId): void {
  useDownloadStore.getState().complete(trackId);
}

export function failDownload(trackId: TrackId): void {
  useDownloadStore.getState().fail(trackId);
}

export function useDownloadPhase(trackId: TrackId): DownloadPhase | undefined {
  return useDownloadStore((s) => s.entries[trackId]?.phase);
}

/** True while the track has not reached a terminal (done or failed) phase. */
export function isInFlight(entry: DownloadEntry): boolean {
  return entry.phase !== 'done' && entry.phase !== 'failed';
}

/** The batch's download entries, failed ones included, sorted by trackId. */
export function useActiveDownloadItems(): DownloadEntry[] {
  const entries = useDownloadStore((s) => s.entries);
  return useMemo(
    () => Object.values(entries).sort((a, b) => a.trackId.localeCompare(b.trackId)),
    [entries],
  );
}

/**
 * The batch phase: the least-advanced in-flight phase while anything is in
 * flight; once settled, 'failed' if any item failed, else 'done'.
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
