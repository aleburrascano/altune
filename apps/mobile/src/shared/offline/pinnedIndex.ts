import { parseTrackId, type TrackId } from '@shared/api-client/ids';
import {
  readVersionedEntries,
  writeDocumentAtomically,
  type SchemaSpec,
} from '@shared/files/durableDocument';
import {
  deviceFileStore,
  type FileStore,
  type StoredDirectory,
  type StoredFile,
} from '@shared/files/fileStore';

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

function offlineDir(): StoredDirectory {
  const dir = fileStore.openDirectory(INDEX_DIR);
  if (!dir.exists) dir.create();
  return dir;
}

function offlineFile(name: string): StoredFile {
  return offlineDir().openFile(name);
}

/** The schema version `saveIndex` stamps on pinned.json. */
export const INDEX_SCHEMA_VERSION = 1;

// Version 0 is the bare trackId -> entry map written before the index carried a version.
const INDEX_SCHEMA: SchemaSpec = {
  current: INDEX_SCHEMA_VERSION,
  migrations: [(bareMap) => ({ schemaVersion: 1, entries: bareMap })],
};

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
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
  const read = readVersionedEntries('[offline]', INDEX_FILE, offlineDir, INDEX_SCHEMA);
  if (read.status !== 'read') return {};
  if (!isPlainObject(read.entries)) {
    console.warn(`[offline] ${INDEX_FILE} is not a pinned index; treating it as empty`);
    return {};
  }
  return narrowIndex(read.entries);
}

/** How long a download's status transitions may coalesce before the index is rewritten. */
export const INDEX_WRITE_DELAY_MS = 2_000;

let pendingEntries: Record<string, PinnedEntry> | null = null;
let pendingTimer: ReturnType<typeof setTimeout> | null = null;

function cancelPendingSave(): void {
  if (pendingTimer !== null) clearTimeout(pendingTimer);
  pendingTimer = null;
  pendingEntries = null;
}

/**
 * Records `entries` as the index to persist, writing at most once per INDEX_WRITE_DELAY_MS however
 * many transitions arrive, so a batch of n downloads does not rewrite the whole index 2n times.
 * Losing an unflushed transition to a kill is safe: launch reconcile rebuilds status from disk.
 */
export function scheduleSaveIndex(entries: Record<string, PinnedEntry>): void {
  pendingEntries = entries;
  pendingTimer ??= setTimeout(flushIndex, INDEX_WRITE_DELAY_MS);
}

/** Writes a scheduled index now, if one is waiting. */
export function flushIndex(): void {
  if (pendingEntries !== null) saveIndex(pendingEntries);
}

/** Writes `entries` immediately, superseding any scheduled write (which holds older state). */
export function saveIndex(entries: Record<string, PinnedEntry>): void {
  cancelPendingSave();
  try {
    const document = { schemaVersion: INDEX_SCHEMA_VERSION, entries };
    writeDocumentAtomically(offlineDir(), INDEX_FILE, JSON.stringify(document));
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
