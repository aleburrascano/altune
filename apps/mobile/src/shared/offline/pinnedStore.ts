import { Directory, File, Paths } from 'expo-file-system';
import { create } from 'zustand';

import { fetchAudioUrls } from '@shared/api-client/audio';

import {
  deleteAllPinned,
  deletePinned,
  downloadPinned,
  findPinned,
  pinnedDirReadable,
} from './pinnedFiles';

export type PinnedStatus = 'queued' | 'downloading' | 'ready' | 'failed';

export type PinnedEntry = {
  trackId: string;
  status: PinnedStatus;
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
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {};
    return parsed as Record<string, PinnedEntry>;
  } catch {
    return {};
  }
}

function saveIndex(entries: Record<string, PinnedEntry>): void {
  try {
    indexFile().write(JSON.stringify(entries));
  } catch {}
}

function needsDownload(entry: PinnedEntry | undefined): boolean {
  return entry === undefined || entry.status === 'failed';
}

export type PinnedState = {
  entries: Record<string, PinnedEntry>;
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
    if (!needsDownload(get().entries[trackId])) return;
    set((s) => {
      const entries = { ...s.entries, [trackId]: { trackId, status: 'queued' as const } };
      saveIndex(entries);
      return { entries, queue: [...s.queue, trackId] };
    });
    void runQueue(set, get);
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

  reconcile: () => {
    if (!pinnedDirReadable()) return;
    const { entries } = get();
    const next: Record<string, PinnedEntry> = {};
    for (const [trackId, entry] of Object.entries(entries)) {
      const file = findPinned(trackId);
      if (file !== null) {
        next[trackId] = { trackId, status: 'ready', uri: file.uri };
      } else if (entry.status === 'queued' || entry.status === 'downloading') {
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
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) throw new Error('no signed url');
    uri = await downloadPinned(trackId, resolved.url);
  } catch {
    uri = undefined;
  }

  const supersededWhileDownloading = get().entries[trackId]?.status !== 'downloading';
  if (supersededWhileDownloading) {
    deletePinned(trackId);
    return;
  }

  mark(uri === undefined ? { trackId, status: 'failed' } : { trackId, status: 'ready', uri });
}

export function pinnedUri(trackId: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  return entry?.status === 'ready' ? entry.uri : undefined;
}

export function repinIfPinned(trackId: string): void {
  const store = usePinnedStore.getState();
  if (store.entries[trackId] === undefined) return;
  store.unpin(trackId);
  store.pin(trackId);
}
