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
  version?: string;
};

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';

function indexFile(): File {
  const dir = new Directory(Paths.document, INDEX_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, INDEX_FILE);
}

const PINNED_STATUSES: Record<PinnedStatus, true> = {
  queued: true,
  downloading: true,
  ready: true,
  failed: true,
};

function isPinnedStatus(value: unknown): value is PinnedStatus {
  return typeof value === 'string' && PINNED_STATUSES[value as PinnedStatus] === true;
}

function narrowEntry(value: unknown): PinnedEntry | null {
  if (typeof value !== 'object' || value === null) return null;
  const record = value as Record<string, unknown>;
  if (typeof record['trackId'] !== 'string' || !isPinnedStatus(record['status'])) return null;
  if (record['uri'] !== undefined && typeof record['uri'] !== 'string') return null;
  if (record['version'] !== undefined && typeof record['version'] !== 'string') return null;
  const entry: PinnedEntry = { trackId: record['trackId'], status: record['status'] };
  if (typeof record['uri'] === 'string') entry.uri = record['uri'];
  if (typeof record['version'] === 'string') entry.version = record['version'];
  return entry;
}

function narrowIndex(parsed: Record<string, unknown>): Record<string, PinnedEntry> {
  const entries: Record<string, PinnedEntry> = {};
  let dropped = 0;
  for (const [trackId, value] of Object.entries(parsed)) {
    const entry = narrowEntry(value);
    if (entry === null) dropped += 1;
    else entries[trackId] = entry;
  }
  if (dropped > 0) console.warn(`[offline] dropped ${dropped} malformed pinned entries at load`);
  return entries;
}

function loadIndex(): Record<string, PinnedEntry> {
  try {
    const file = indexFile();
    if (!file.exists) return {};
    const parsed: unknown = JSON.parse(file.textSync());
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {};
    return narrowIndex(parsed as Record<string, unknown>);
  } catch {
    return {};
  }
}

function saveIndex(entries: Record<string, PinnedEntry>): void {
  try {
    indexFile().write(JSON.stringify(entries));
  } catch {
    console.warn('[offline] failed to persist pinned index; keeping in-memory only');
  }
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

function versionDisagrees(entry: PinnedEntry | undefined, expectedVersion?: string): boolean {
  if (entry?.status !== 'ready') return false;
  return (
    expectedVersion !== undefined && expectedVersion !== '' && entry.version !== expectedVersion
  );
}

export function pinnedUri(trackId: string, expectedVersion?: string): string | undefined {
  const entry = usePinnedStore.getState().entries[trackId];
  if (entry?.status !== 'ready') return undefined;
  if (versionDisagrees(entry, expectedVersion)) return undefined;
  return entry.uri;
}

export function repinIfStale(trackId: string, expectedVersion?: string): void {
  const entry = usePinnedStore.getState().entries[trackId];
  if (versionDisagrees(entry, expectedVersion)) repinIfPinned(trackId);
}

export function repinIfPinned(trackId: string): void {
  const store = usePinnedStore.getState();
  if (store.entries[trackId] === undefined) return;
  store.unpin(trackId);
  store.pin(trackId);
}
