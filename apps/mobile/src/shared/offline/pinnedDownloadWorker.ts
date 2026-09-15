import { fetchAudioUrls } from '@shared/api-client/audio';
import type { TrackId } from '@shared/api-client/ids';

import { deletePinned, downloadPinned } from './pinnedFiles';
import { type PinnedEntry, saveIndex } from './pinnedIndex';

// The sequential background download worker: drains the store's queue one track
// at a time, guarded by isWorking so concurrent triggers never run two drains.

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
    for (;;) {
      const trackId = get().queue[0];
      if (trackId === undefined) break;
      set((s) => ({ queue: s.queue.slice(1) }));
      await downloadOne(trackId, set, get);
    }
  } finally {
    set({ isWorking: false });
  }
}

async function downloadOne(trackId: TrackId, set: Setter, get: Getter): Promise<void> {
  if (get().entries[trackId] === undefined) return;

  const mark = (entry: PinnedEntry): void => {
    set((s) => {
      if (s.entries[trackId] === undefined) return {};
      const entries = { ...s.entries, [trackId]: entry };
      saveIndex(entries);
      return { entries };
    });
  };

  mark({ trackId, status: 'downloading' });

  let uri: string | undefined;
  let version = '';
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) throw new Error('no signed url');
    version = resolved.version;
    uri = await downloadPinned(trackId, resolved.url);
  } catch {
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
