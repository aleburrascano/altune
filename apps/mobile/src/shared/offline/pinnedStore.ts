/**
 * Offline downloads — which tracks the user has pinned, and where they live.
 *
 * The index is persisted alongside the audio so a relaunch knows what it has
 * without re-listing and re-parsing the directory on every read. The FILES are
 * the source of truth though: `reconcile()` runs once at startup and drops any
 * entry whose file has gone (a restore-from-backup, a manual clean), so the UI
 * can never claim a track is available offline when it isn't.
 *
 * Downloads are sequential by design. Three concurrent downloads of a 10 MB
 * track on a phone hotspot is how you get three timeouts instead of one file.
 */
import { Directory, File, Paths } from 'expo-file-system';
import { create } from 'zustand';

import { fetchAudioUrls } from '@shared/api-client/audio';

import { deleteAllPinned, deletePinned, downloadPinned, findPinned } from './pinnedFiles';

export type PinnedStatus = 'queued' | 'downloading' | 'ready' | 'failed';

export type PinnedEntry = {
  trackId: string;
  status: PinnedStatus;
  /** file:// URI, present once ready. */
  uri?: string;
};

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';

function indexFile(): File {
  const dir = new Directory(Paths.document, INDEX_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, INDEX_FILE);
}

function loadIndex(): Record<string, PinnedEntry> {
  try {
    const file = indexFile();
    if (!file.exists) return {};
    const parsed: unknown = JSON.parse(file.textSync());
    if (typeof parsed !== 'object' || parsed === null) return {};
    return parsed as Record<string, PinnedEntry>;
  } catch {
    return {};
  }
}

function saveIndex(entries: Record<string, PinnedEntry>): void {
  try {
    indexFile().write(JSON.stringify(entries));
  } catch {
    // The files are the source of truth; a lost index costs a reconcile, not data.
  }
}

export type PinnedState = {
  entries: Record<string, PinnedEntry>;
  /** Track ids waiting on the sequential worker. */
  queue: string[];
  isWorking: boolean;
  pin: (trackId: string) => void;
  pinMany: (trackIds: readonly string[]) => void;
  unpin: (trackId: string) => void;
  unpinAll: () => void;
  reconcile: () => void;
};

export const usePinnedStore = create<PinnedState>((set, get) => ({
  entries: loadIndex(),
  queue: [],
  isWorking: false,

  pin: (trackId) => {
    const existing = get().entries[trackId];
    if (existing?.status === 'ready' || existing?.status === 'downloading') return;
    set((s) => {
      const entries = { ...s.entries, [trackId]: { trackId, status: 'queued' as const } };
      saveIndex(entries);
      return { entries, queue: [...s.queue, trackId] };
    });
    void runQueue(set, get);
  },

  pinMany: (trackIds) => {
    const { entries } = get();
    const fresh = trackIds.filter(
      (id) => entries[id]?.status !== 'ready' && entries[id]?.status !== 'downloading',
    );
    if (fresh.length === 0) return;
    set((s) => {
      const next = { ...s.entries };
      for (const id of fresh) next[id] = { trackId: id, status: 'queued' };
      saveIndex(next);
      return { entries: next, queue: [...s.queue, ...fresh] };
    });
    void runQueue(set, get);
  },

  unpin: (trackId) => {
    deletePinned(trackId);
    set((s) => {
      const entries = { ...s.entries };
      delete entries[trackId];
      saveIndex(entries);
      return { entries, queue: s.queue.filter((id) => id !== trackId) };
    });
  },

  unpinAll: () => {
    deleteAllPinned();
    saveIndex({});
    set({ entries: {}, queue: [] });
  },

  // Files win over the index. An entry whose file vanished must not keep
  // claiming the track is available offline.
  reconcile: () => {
    const { entries } = get();
    const next: Record<string, PinnedEntry> = {};
    for (const [trackId, entry] of Object.entries(entries)) {
      const file = findPinned(trackId);
      if (file !== null) {
        next[trackId] = { trackId, status: 'ready', uri: file.uri };
      } else if (entry.status === 'queued' || entry.status === 'downloading') {
        // Interrupted by a kill — re-queue rather than silently forgetting.
        next[trackId] = { trackId, status: 'queued' };
      }
    }
    saveIndex(next);
    const requeue = Object.values(next)
      .filter((e) => e.status === 'queued')
      .map((e) => e.trackId);
    set({ entries: next, queue: requeue });
    if (requeue.length > 0) void runQueue(set, get);
  },
}));

type Setter = (partial: Partial<PinnedState> | ((s: PinnedState) => Partial<PinnedState>)) => void;
type Getter = () => PinnedState;

/** Sequential download worker. One at a time, and re-entrant-safe via isWorking. */
async function runQueue(set: Setter, get: Getter): Promise<void> {
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

async function downloadOne(trackId: string, set: Setter, get: Getter): Promise<void> {
  // Unpinned while it sat in the queue.
  if (get().entries[trackId] === undefined) return;

  const mark = (entry: PinnedEntry): void => {
    set((s) => {
      // Don't resurrect an entry the user unpinned mid-download.
      if (s.entries[trackId] === undefined) return {};
      const entries = { ...s.entries, [trackId]: entry };
      saveIndex(entries);
      return { entries };
    });
  };

  mark({ trackId, status: 'downloading' });
  try {
    // The same presigned-URL path playback uses. A pinned copy is a real
    // download of the real object, not a second representation of it.
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) throw new Error('no signed url');
    const uri = await downloadPinned(trackId, resolved.url);
    mark({ trackId, status: 'ready', uri });
  } catch {
    mark({ trackId, status: 'failed' });
  }
}

/** The local file for a track, if it is pinned and ready. Read outside React —
 *  the playback path is not a component. */
export function pinnedUri(trackId: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  return entry?.status === 'ready' ? entry.uri : undefined;
}
