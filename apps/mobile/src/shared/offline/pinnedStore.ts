import { create } from 'zustand';

import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { onIdentityChange, onSignOut } from '@shared/session/signOutCleanup';
import { onKillSwitchChange } from '@shared/killSwitch/killSwitch';

import { runDownloadQueue } from './pinnedDownloadWorker';
import {
  deleteAllPinned,
  deletePinned,
  deletePinnedMany,
  deleteAbandonedDownloads,
  pinStorageFull,
  pinnedFilesByTrackId,
} from './pinnedFiles';
import {
  type PinnedEntry,
  flushIndex,
  loadIndex,
  queuedEntry,
  readOwner,
  readyEntry,
  saveIndex,
  scheduleSaveIndex,
  writeOwner,
} from './pinnedIndex';

// Re-exported through the offline store port so the UI reads the pinned-download
// byte total (and its formatter) from usePinnedStore instead of binding to the
// filesystem adapter directly. The total comes from the files on disk, not the
// index; the index keeps a ready entry whose delete failed so the two agree.
export { formatBytes, pinnedBytes as pinnedByteTotal } from './pinnedFiles';

export type { PinnedEntry, PinnedStatus } from './pinnedIndex';

// Ready entries whose file a delete pass could not remove. Only ready ones are
// kept: an in-flight download is still cancelled by dropping its entry.
function readyWithFileOnDisk(entries: Record<string, PinnedEntry>): Record<string, PinnedEntry> {
  const kept: Record<string, PinnedEntry> = {};
  const onDisk = pinnedFilesByTrackId();
  if (onDisk === null) return kept;
  for (const [trackId, entry] of Object.entries(entries)) {
    if (entry.status === 'ready' && onDisk.has(trackId)) kept[trackId] = entry;
  }
  return kept;
}

function needsDownload(entry: PinnedEntry | undefined): boolean {
  return entry === undefined || entry.status === 'failed';
}

// Only a ready entry records which audio version it downloaded, so a re-listed file keeps the
// version its own download stamped and nothing else inherits one.
function recordedVersion(entry: PinnedEntry): string | undefined {
  return entry.status === 'ready' ? entry.version : undefined;
}

/** Whether a pin was taken, or refused because pinned storage is full. */
export type PinAdmission = 'accepted' | 'storage-full';

/**
 * How a pinMany batch ended: how many tracks it queued, and how many of those failed.
 * `refused` is set when the whole batch was turned away before anything was queued.
 */
export type PinBatchResult = { requested: number; failed: number; refused?: 'storage-full' };

function refuseForStorage(count: number): void {
  console.warn(`[offline] refused to pin ${count} track(s): pinned storage is full`);
}

// Resolves once every id has left queued/downloading. An id no longer indexed was
// unpinned (or signed out) mid-batch: that is a cancellation, not a failure.
function awaitBatchSettled(trackIds: readonly TrackId[]): Promise<PinBatchResult> {
  return new Promise((resolve) => {
    const check = (entries: Record<string, PinnedEntry>): boolean => {
      let failed = 0;
      for (const id of trackIds) {
        const status = entries[id]?.status;
        if (status === 'queued' || status === 'downloading') return false;
        if (status === 'failed') failed += 1;
      }
      resolve({ requested: trackIds.length, failed });
      return true;
    };
    if (check(usePinnedStore.getState().entries)) return;
    const unsubscribe = usePinnedStore.subscribe((state) => {
      if (check(state.entries)) unsubscribe();
    });
  });
}

/** Downloads removed in one pass before the removal yields the thread. */
const UNPIN_CHUNK = 64;
/**
 * No further chunk starts after this. A filesystem slow enough to bind it would otherwise hold a
 * removal of thousands for minutes; the downloads left behind are reported rather than retried.
 */
const UNPIN_DEADLINE_MS = 30_000;

/** How an unpinMany batch ended: how many removals were asked for, and how many are still downloaded. */
export type UnpinBatchResult = { requested: number; failed: number };

/** Whether a remove-all pass deleted every pinned file, or left behind ones it could not delete. */
export type UnpinAllOutcome = 'all-removed' | 'partial';

type UnpinSetter = (updater: (s: PinnedState) => Partial<PinnedState>) => void;

// A ready download whose file survived its delete stays indexed, as unpin keeps it, so its bytes
// are still counted and it can be removed again rather than orphaning them.
function withoutRemoved(
  entries: Record<string, PinnedEntry>,
  trackIds: readonly TrackId[],
  stillOnDisk: ReadonlySet<string>,
): Record<string, PinnedEntry> {
  const kept = { ...entries };
  for (const trackId of trackIds) {
    if (stillOnDisk.has(trackId) && kept[trackId]?.status === 'ready') continue;
    delete kept[trackId];
  }
  return kept;
}

// Deletes one chunk's files from a single directory listing and drops their entries; returns how
// many of the chunk are no longer downloaded. Synchronous throughout, so the download worker
// cannot interleave between the listing and the entries it produces.
function removeChunk(set: UnpinSetter, get: () => PinnedState, chunk: readonly TrackId[]): number {
  const stillOnDisk = deletePinnedMany(chunk);
  const entries = withoutRemoved(get().entries, chunk, stillOnDisk);
  scheduleSaveIndex(entries);
  set((s) => ({ entries, queue: s.queue.filter((trackId) => entries[trackId] !== undefined) }));
  return chunk.filter((trackId) => entries[trackId] === undefined).length;
}

// Hands the thread back so the rows removed so far can paint before the next chunk runs.
function yieldToThread(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

// Returns how many of `trackIds` are no longer downloaded once the passes end.
async function removeInChunks(
  set: UnpinSetter,
  get: () => PinnedState,
  trackIds: readonly TrackId[],
): Promise<number> {
  const startedAt = performance.now();
  let removed = 0;
  for (let from = 0; from < trackIds.length; from += UNPIN_CHUNK) {
    if (performance.now() - startedAt >= UNPIN_DEADLINE_MS) break;
    removed += removeChunk(set, get, trackIds.slice(from, from + UNPIN_CHUNK));
    await yieldToThread();
  }
  flushIndex();
  return removed;
}

export type PinnedState = {
  entries: Record<string, PinnedEntry>;
  queue: TrackId[];
  isWorking: boolean;
  /**
   * How the last remove-all pass ended, undefined until one has run. Kept because a delete that
   * left files behind is not visible in `entries` alone, and the settings row reports it.
   */
  lastUnpinAll: UnpinAllOutcome | undefined;
  /** Queues the track if it needs a download, unless pinned storage is full. */
  pin: (trackId: TrackId) => PinAdmission;
  /** Queues the tracks that still need a download; resolves when that batch settles. */
  pinMany: (trackIds: readonly TrackId[]) => Promise<PinBatchResult>;
  unpin: (trackId: TrackId) => void;
  /** Removes the tracks' downloads in bounded passes; resolves once the last pass has settled. */
  unpinMany: (trackIds: readonly TrackId[]) => Promise<UnpinBatchResult>;
  /** Removes every download; reports whether any file survived its delete. */
  unpinAll: () => UnpinAllOutcome;
  reconcile: () => void;
};

export const usePinnedStore = create<PinnedState>((set, get) => ({
  entries: loadIndex(),
  queue: [],
  isWorking: false,
  lastUnpinAll: undefined,

  pin: (trackId) => {
    if (!needsDownload(get().entries[trackId])) return 'accepted';
    if (pinStorageFull()) {
      refuseForStorage(1);
      return 'storage-full';
    }
    set((s) => {
      const entries = { ...s.entries, [trackId]: queuedEntry(trackId) };
      saveIndex(entries);
      return { entries, queue: [...s.queue, trackId] };
    });
    void runDownloadQueue(set, get);
    return 'accepted';
  },

  pinMany: (trackIds) => {
    const { entries } = get();
    const fresh = trackIds.filter((id) => needsDownload(entries[id]));
    if (fresh.length === 0) return Promise.resolve({ requested: 0, failed: 0 });
    if (pinStorageFull()) {
      refuseForStorage(fresh.length);
      return Promise.resolve({ requested: 0, failed: 0, refused: 'storage-full' });
    }
    set((s) => {
      const next = { ...s.entries };
      for (const id of fresh) next[id] = queuedEntry(id);
      saveIndex(next);
      return { entries: next, queue: [...s.queue, ...fresh] };
    });
    const settled = awaitBatchSettled([...new Set(fresh)]);
    void runDownloadQueue(set, get);
    return settled;
  },

  unpin: (trackId) => {
    // A ready track whose file survived stays indexed, so it is still counted
    // and can be removed again rather than orphaning its bytes.
    if (!deletePinned(trackId) && get().entries[trackId]?.status === 'ready') return;
    set((s) => {
      const entries = { ...s.entries };
      delete entries[trackId];
      saveIndex(entries);
      return { entries, queue: s.queue.filter((id) => id !== trackId) };
    });
  },

  unpinMany: async (trackIds) => {
    const ids = [...new Set(trackIds)];
    const removed = await removeInChunks(set, get, ids);
    const failed = ids.length - removed;
    if (failed > 0) {
      console.warn(`[offline] ${failed} of ${ids.length} download(s) could not be removed`);
    }
    return { requested: ids.length, failed };
  },

  unpinAll: () => {
    const allRemoved = deleteAllPinned();
    const outcome = allRemoved ? 'all-removed' : 'partial';
    const survivors = allRemoved ? {} : readyWithFileOnDisk(get().entries);
    saveIndex(survivors);
    set({ entries: survivors, queue: [], lastUnpinAll: outcome });
    return outcome;
  },

  reconcile: () => {
    // One listing for the whole index: launch cost stays linear in pinned entries plus files.
    const onDisk = pinnedFilesByTrackId();
    if (onDisk === null) return;
    const { entries, isWorking } = get();
    if (!isWorking) deleteAbandonedDownloads();
    const next: Record<string, PinnedEntry> = {};
    for (const [key, entry] of Object.entries(entries)) {
      // The index key is the source of truth for the id. A key outside the TrackId shape can
      // never hold a file or be downloaded, so it is dropped here.
      const parsed = parseTrackId(key);
      if (!parsed.ok) continue;
      const trackId = parsed.id;
      const file = onDisk.get(trackId);
      if (file !== undefined) {
        next[trackId] = readyEntry(trackId, file.uri, recordedVersion(entry));
      } else if (entry.status === 'queued' || entry.status === 'downloading') {
        next[trackId] = queuedEntry(trackId);
      }
    }
    saveIndex(next);
    const requeue = Object.values(next)
      .filter((e) => e.status === 'queued')
      .map((e) => e.trackId);
    set({ entries: next, queue: requeue });
    if (requeue.length > 0) void runDownloadQueue(set, get);
  },
}));

// Sign-out clears downloads; this is best effort (the app can be killed first),
// so claimPinnedDownloads is the durable boundary.
onSignOut(() => void usePinnedStore.getState().unpinAll());

// The worker stops draining while the offline-download kill switch is off; the
// tracks it left queued resume as soon as the switch is turned back on.
onKillSwitchChange((loop, enabled) => {
  if (loop !== 'offlineDownloads' || !enabled) return;
  if (usePinnedStore.getState().queue.length > 0) {
    void runDownloadQueue(usePinnedStore.setState, usePinnedStore.getState);
  }
});

/**
 * Makes `userId` the owner of the on-disk downloads before anything of theirs is
 * shown. Downloads left by another account, or of unknown owner (e.g. an app
 * killed before its sign-out cleanup finished), are deleted, never adopted. Files
 * that fail to delete are dropped from the index anyway: their bytes stay visible
 * to the byte total (and removable), but never as this user's downloads.
 */
export function claimPinnedDownloads(userId: string): void {
  if (readOwner() === userId) return;
  usePinnedStore.getState().unpinAll();
  saveIndex({});
  usePinnedStore.setState({ entries: {} });
  writeOwner(userId);
}

onIdentityChange((userId) => {
  if (userId !== null) claimPinnedDownloads(userId);
});

// An absent or empty expectation is "nothing to check against", not a mismatch, so a track the
// server has never re-acquired is never re-downloaded on the strength of a missing version.
function versionDisagrees(localVersion?: string, expectedVersion?: string): boolean {
  return expectedVersion !== undefined && expectedVersion !== '' && localVersion !== expectedVersion;
}

/**
 * The downloaded file to play for `trackId`, or undefined to stream it. Refusing a stale copy
 * and re-pinning it are one call rather than two, so no caller can order them the wrong way
 * round and serve the bytes the server has already replaced.
 */
export function resolvePinnedUri(trackId: TrackId, expectedVersion?: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  if (entry?.status !== 'ready') return undefined;
  if (versionDisagrees(entry.version, expectedVersion)) {
    repinIfPinned(trackId);
    return undefined;
  }
  return entry.uri;
}

export function repinIfPinned(trackId: TrackId): void {
  const store = usePinnedStore.getState();
  if (store.entries[trackId] === undefined) return;
  store.unpin(trackId);
  store.pin(trackId);
}
