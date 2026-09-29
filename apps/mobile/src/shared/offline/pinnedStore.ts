import { create } from 'zustand';

import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { onSignOut } from '@shared/session/signOutCleanup';
import { onKillSwitchChange } from '@shared/killSwitch/killSwitch';

import { offlineDownloadsSupported } from './offlineSupport';
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
  changedEntryIds,
  flushIndex,
  loadIndex,
  queuedEntry,
  readOwner,
  readyEntry,
  saveIndex,
  scheduleSaveIndex,
  tagChangedEntries,
  writeOwner,
} from './pinnedIndex';

export { formatBytes, pinnedBytes as pinnedByteTotal } from './pinnedFiles';

export type { PinnedEntry, PinnedStatus } from './pinnedIndex';

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

function runDownloadQueueIfSupported(
  set: Parameters<typeof runDownloadQueue>[0],
  get: Parameters<typeof runDownloadQueue>[1],
): void {
  if (offlineDownloadsSupported) void runDownloadQueue(set, get);
}

function recordedVersion(entry: PinnedEntry): string | undefined {
  return entry.status === 'ready' ? entry.version : undefined;
}

export type PinAdmission = 'accepted' | 'storage-full';

export type PinBatchResult = { requested: number; failed: number; refused?: 'storage-full' };

function refuseForStorage(count: number): void {
  console.warn(`[offline] refused to pin ${count} track(s): pinned storage is full`);
}

function setMembership(members: Set<string>, id: string, isMember: boolean): void {
  if (isMember) members.add(id);
  else members.delete(id);
}

function awaitBatchSettled(trackIds: readonly TrackId[]): Promise<PinBatchResult> {
  return new Promise((resolve) => {
    const batch = new Set<string>(trackIds);
    const pending = new Set<string>();
    const failed = new Set<string>();
    const classify = (entries: Record<string, PinnedEntry>, id: string): void => {
      const status = entries[id]?.status;
      setMembership(pending, id, status === 'queued' || status === 'downloading');
      setMembership(failed, id, status === 'failed');
    };
    const settleIfDone = (): boolean => {
      if (pending.size > 0) return false;
      resolve({ requested: trackIds.length, failed: failed.size });
      return true;
    };
    const initial = usePinnedStore.getState().entries;
    for (const id of batch) classify(initial, id);
    if (settleIfDone()) return;
    const unsubscribe = usePinnedStore.subscribe((state, previous) => {
      if (state.entries === previous.entries) return;
      const changed = changedEntryIds(state.entries);
      for (const id of changed ?? batch) {
        if (batch.has(id)) classify(state.entries, id);
      }
      if (settleIfDone()) unsubscribe();
    });
  });
}

const UNPIN_CHUNK = 64;
const UNPIN_DEADLINE_MS = 30_000;

export type UnpinBatchResult = { requested: number; failed: number };

export type UnpinAllOutcome = 'all-removed' | 'partial';

type UnpinSetter = (updater: (s: PinnedState) => Partial<PinnedState>) => void;

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

function removeChunk(set: UnpinSetter, get: () => PinnedState, chunk: readonly TrackId[]): number {
  const stillOnDisk = deletePinnedMany(chunk);
  const entries = withoutRemoved(get().entries, chunk, stillOnDisk);
  scheduleSaveIndex(entries);
  tagChangedEntries(entries, chunk);
  set((s) => ({ entries, queue: s.queue.filter((trackId) => entries[trackId] !== undefined) }));
  return chunk.filter((trackId) => entries[trackId] === undefined).length;
}

function yieldToThread(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

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
  lastUnpinAll: UnpinAllOutcome | undefined;
  pin: (trackId: TrackId) => PinAdmission;
  pinMany: (trackIds: readonly TrackId[]) => Promise<PinBatchResult>;
  unpin: (trackId: TrackId) => void;
  unpinMany: (trackIds: readonly TrackId[]) => Promise<UnpinBatchResult>;
  unpinAll: () => UnpinAllOutcome;
  reconcile: () => void;
};

export const usePinnedStore = create<PinnedState>((set, get) => ({
  entries: offlineDownloadsSupported ? loadIndex() : {},
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
      tagChangedEntries(entries, [trackId]);
      saveIndex(entries);
      return { entries, queue: [...s.queue, trackId] };
    });
    runDownloadQueueIfSupported(set, get);
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
      tagChangedEntries(next, fresh);
      saveIndex(next);
      return { entries: next, queue: [...s.queue, ...fresh] };
    });
    const settled = awaitBatchSettled([...new Set(fresh)]);
    runDownloadQueueIfSupported(set, get);
    return settled;
  },

  unpin: (trackId) => {
    if (!deletePinned(trackId) && get().entries[trackId]?.status === 'ready') return;
    set((s) => {
      const entries = { ...s.entries };
      delete entries[trackId];
      tagChangedEntries(entries, [trackId]);
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
    const onDisk = pinnedFilesByTrackId();
    if (onDisk === null) return;
    const { entries, isWorking } = get();
    if (!isWorking) deleteAbandonedDownloads();
    const next: Record<string, PinnedEntry> = {};
    for (const [key, entry] of Object.entries(entries)) {
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
    if (requeue.length > 0) runDownloadQueueIfSupported(set, get);
  },
}));

onSignOut(() => void usePinnedStore.getState().unpinAll());

onKillSwitchChange((loop, enabled) => {
  if (loop !== 'offlineDownloads' || !enabled) return;
  if (usePinnedStore.getState().queue.length > 0) {
    runDownloadQueueIfSupported(usePinnedStore.setState, usePinnedStore.getState);
  }
});

export function claimPinnedDownloads(userId: string): void {
  if (readOwner() === userId) return;
  usePinnedStore.getState().unpinAll();
  saveIndex({});
  usePinnedStore.setState({ entries: {} });
  writeOwner(userId);
}

function versionDisagrees(localVersion?: string, expectedVersion?: string): boolean {
  return (
    expectedVersion !== undefined && expectedVersion !== '' && localVersion !== expectedVersion
  );
}

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
