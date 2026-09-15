import { create } from 'zustand';

import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { onSignOut } from '@shared/auth/signOutCleanup';

import { runDownloadQueue } from './pinnedDownloadWorker';
import { deleteAllPinned, deletePinned, findPinned, pinnedDirReadable } from './pinnedFiles';
import { type PinnedEntry, loadIndex, readOwner, saveIndex, writeOwner } from './pinnedIndex';

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
  for (const [trackId, entry] of Object.entries(entries)) {
    if (entry.status === 'ready' && findPinned(trackId) !== null) kept[trackId] = entry;
  }
  return kept;
}

function needsDownload(entry: PinnedEntry | undefined): boolean {
  return entry === undefined || entry.status === 'failed';
}

export type PinnedState = {
  entries: Record<string, PinnedEntry>;
  queue: TrackId[];
  isWorking: boolean;
  pin: (trackId: TrackId) => void;
  pinMany: (trackIds: readonly TrackId[]) => void;
  unpin: (trackId: TrackId) => void;
  unpinAll: () => void;
  reconcile: () => void;
};

export const usePinnedStore = create<PinnedState>((set, get) => ({
  entries: loadIndex(),
  queue: [],
  isWorking: false,

  pin: (trackId) => {
    if (!needsDownload(get().entries[trackId])) return;
    set((s) => {
      const entries = { ...s.entries, [trackId]: { trackId, status: 'queued' as const } };
      saveIndex(entries);
      return { entries, queue: [...s.queue, trackId] };
    });
    void runDownloadQueue(set, get);
  },

  pinMany: (trackIds) => {
    const { entries } = get();
    const fresh = trackIds.filter((id) => needsDownload(entries[id]));
    if (fresh.length === 0) return;
    set((s) => {
      const next = { ...s.entries };
      for (const id of fresh) next[id] = { trackId: id, status: 'queued' };
      saveIndex(next);
      return { entries: next, queue: [...s.queue, ...fresh] };
    });
    void runDownloadQueue(set, get);
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

  unpinAll: () => {
    const survivors = deleteAllPinned() ? {} : readyWithFileOnDisk(get().entries);
    saveIndex(survivors);
    set({ entries: survivors, queue: [] });
  },

  reconcile: () => {
    if (!pinnedDirReadable()) return;
    const { entries } = get();
    const next: Record<string, PinnedEntry> = {};
    for (const [key, entry] of Object.entries(entries)) {
      // The index key is the source of truth for the id. A key outside the TrackId shape can
      // never hold a file (findPinned refuses it) or be downloaded, so it is dropped here.
      const parsed = parseTrackId(key);
      if (!parsed.ok) continue;
      const trackId = parsed.id;
      const file = findPinned(trackId);
      if (file !== null) {
        next[trackId] = { ...entry, trackId, status: 'ready', uri: file.uri };
      } else if (entry.status === 'queued' || entry.status === 'downloading') {
        next[trackId] = { trackId, status: 'queued' };
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
onSignOut(() => usePinnedStore.getState().unpinAll());

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

function versionDisagrees(entry: PinnedEntry | undefined, expectedVersion?: string): boolean {
  if (entry?.status !== 'ready') return false;
  return (
    expectedVersion !== undefined && expectedVersion !== '' && entry.version !== expectedVersion
  );
}

// Call after repinIfStale: a stale ready entry has by then been requeued, so this returns undefined and the caller streams.
export function pinnedUri(trackId: TrackId, expectedVersion?: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  if (entry?.status !== 'ready') return undefined;
  if (versionDisagrees(entry, expectedVersion)) return undefined;
  return entry.uri;
}

// Call before pinnedUri: this synchronously moves a stale entry off 'ready', which is what makes that read skip it.
export function repinIfStale(trackId: TrackId, expectedVersion?: string): void {
  const entry = usePinnedStore.getState().entries[trackId];
  if (versionDisagrees(entry, expectedVersion)) repinIfPinned(trackId);
}

export function repinIfPinned(trackId: TrackId): void {
  const store = usePinnedStore.getState();
  if (store.entries[trackId] === undefined) return;
  store.unpin(trackId);
  store.pin(trackId);
}
