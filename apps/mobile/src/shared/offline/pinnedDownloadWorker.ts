import { fetchAudioUrls } from '@shared/api-client/audio';
import type { TrackId } from '@shared/api-client/ids';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';

import {
  deletePinned,
  downloadPinned,
  pinStorageFull,
  unsignedUrl,
  withPinnedBytesCached,
} from './pinnedFiles';
import { type PinnedEntry, flushIndex, scheduleSaveIndex } from './pinnedIndex';

// The sequential background download worker: drains the store's queue one track
// at a time, guarded by isWorking so concurrent triggers never run two drains.
// The offline-download kill switch is read before each track: switched off, the
// drain stops and leaves the rest queued (a download already running finishes),
// and the store restarts the drain when the switch comes back on.

type QueueState = {
  entries: Record<string, PinnedEntry>;
  queue: TrackId[];
  isWorking: boolean;
};

type Setter = (partial: Partial<QueueState> | ((s: QueueState) => Partial<QueueState>)) => void;
type Getter = () => QueueState;

export async function runDownloadQueue(set: Setter, get: Getter): Promise<void> {
  if (get().isWorking) return;
  set({ isWorking: true });
  try {
    // One drain measures the pinned directory once: the isWorking guard above makes it the only
    // pass open, so its running byte total is what every track's room check reads.
    await withPinnedBytesCached(() => drainQueue(set, get));
  } finally {
    set({ isWorking: false });
    // The drain's coalesced status transitions reach disk as soon as the queue is empty.
    flushIndex();
  }
}

async function drainQueue(set: Setter, get: Getter): Promise<void> {
  for (;;) {
    const trackId = get().queue[0];
    if (trackId === undefined || !isLoopEnabled('offlineDownloads')) break;
    set((s) => ({ queue: s.queue.slice(1) }));
    await downloadOne(trackId, set, get);
  }
}

async function downloadOne(trackId: TrackId, set: Setter, get: Getter): Promise<void> {
  if (get().entries[trackId] === undefined) return;

  const mark = (entry: PinnedEntry): void => {
    set((s) => {
      if (s.entries[trackId] === undefined) return {};
      const entries = { ...s.entries, [trackId]: entry };
      scheduleSaveIndex(entries);
      return { entries };
    });
  };

  mark({ trackId, status: 'downloading' });

  let uri: string | undefined;
  let url: string | undefined;
  let version = '';
  try {
    // Storage can fill while a batch drains, so the room check is repeated at each track's turn.
    if (pinStorageFull()) throw new Error('pinned storage is full');
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) throw new Error('no signed url');
    version = resolved.version;
    url = resolved.url;
    uri = await downloadPinned(trackId, resolved.url);
  } catch (error) {
    console.warn(`[offline] pinned download failed for track ${trackId}`, {
      url: unsignedUrl(url),
      error,
    });
    uri = undefined;
  }

  const supersededWhileDownloading = get().entries[trackId]?.status !== 'downloading';
  if (supersededWhileDownloading) {
    deletePinned(trackId);
    return;
  }

  mark(
    uri === undefined ? { trackId, status: 'failed' } : { trackId, status: 'ready', uri, version },
  );
}
