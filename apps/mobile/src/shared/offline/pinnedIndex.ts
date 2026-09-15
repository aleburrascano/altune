import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import { deviceFileStore, type FileStore, type StoredFile } from '@shared/files/fileStore';

// Persistence of the pinned-download index and its owner marker: the on-disk
// shape, how a loaded file is narrowed back into entries, and best-effort writes.

export type PinnedStatus = 'queued' | 'downloading' | 'ready' | 'failed';

export type PinnedEntry = {
  trackId: TrackId;
  status: PinnedStatus;
  uri?: string;
  version?: string;
};

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';
const OWNER_FILE = 'pinned-owner';

let fileStore: FileStore = deviceFileStore;

/** Points the index and owner marker at `store`; with no argument, back at the device filesystem. */
export function setPinnedIndexFileStore(store: FileStore = deviceFileStore): void {
  fileStore = store;
}

function offlineFile(name: string): StoredFile {
  const dir = fileStore.openDirectory(INDEX_DIR);
  if (!dir.exists) dir.create();
  return dir.openFile(name);
}

function indexFile(): StoredFile {
  return offlineFile(INDEX_FILE);
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

// The index file is where a raw track id comes back off disk, so it is re-branded here: an entry
// whose id is not a valid TrackId is malformed, like one with an unknown status.
function narrowEntry(value: unknown): PinnedEntry | null {
  if (typeof value !== 'object' || value === null) return null;
  const record = value as Record<string, unknown>;
  if (typeof record['trackId'] !== 'string' || !isPinnedStatus(record['status'])) return null;
  if (record['uri'] !== undefined && typeof record['uri'] !== 'string') return null;
  if (record['version'] !== undefined && typeof record['version'] !== 'string') return null;
  const trackId = parseTrackId(record['trackId']);
  if (!trackId.ok) return null;
  const entry: PinnedEntry = { trackId: trackId.id, status: record['status'] };
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

export function loadIndex(): Record<string, PinnedEntry> {
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

export function saveIndex(entries: Record<string, PinnedEntry>): void {
  try {
    indexFile().write(JSON.stringify(entries));
  } catch {
    console.warn('[offline] failed to persist pinned index; keeping in-memory only');
  }
}

// The index and audio files are keyed by trackId only, so ownership is recorded
// beside them. It is written only after a claim has emptied the store, which makes
// a missing or unreadable owner mean "unknown", and unknown is treated as foreign.
export function readOwner(): string | null {
  try {
    const file = offlineFile(OWNER_FILE);
    return file.exists ? file.textSync() : null;
  } catch {
    return null;
  }
}

export function writeOwner(userId: string): void {
  try {
    offlineFile(OWNER_FILE).write(userId);
  } catch {
    console.warn('[offline] failed to persist pinned owner; downloads will be cleared next launch');
  }
}
