import { isSafeId, parseTrackId, type TrackId } from '@shared/api-client/ids';
import {
  readVersionedEntries,
  writeDocumentAtomically,
  type SchemaSpec,
} from '@shared/files/durableDocument';
import {
  createFileStoreSlot,
  type FileStore,
  type StoredDirectory,
  type StoredFile,
} from '@shared/files/fileStore';

// Persistence of the pinned-download index and its owner marker: the on-disk
// shape, how a loaded file is narrowed back into entries, and best-effort writes.

/**
 * One pinned track, carrying only the fields its status has: a ready download names the file it
 * wrote, and a track that is not ready cannot name one, so no reader can serve a stale or absent
 * uri. Build these with the constructors below rather than by hand.
 */
export type PinnedEntry =
  | { trackId: TrackId; status: 'ready'; uri: string; version?: string }
  | { trackId: TrackId; status: 'failed' }
  | { trackId: TrackId; status: 'queued' | 'downloading' };

export type PinnedStatus = PinnedEntry['status'];

export function readyEntry(trackId: TrackId, uri: string, version?: string): PinnedEntry {
  if (version === undefined) return { trackId, status: 'ready', uri };
  return { trackId, status: 'ready', uri, version };
}

export function queuedEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'queued' };
}

export function downloadingEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'downloading' };
}

export function failedEntry(trackId: TrackId): PinnedEntry {
  return { trackId, status: 'failed' };
}

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';
const OWNER_FILE = 'pinned-owner';

const fileStore = createFileStoreSlot();

/** Points the index and owner marker at `store`; with no argument, back at the device filesystem. */
export function setPinnedIndexFileStore(store?: FileStore): void {
  fileStore.set(store);
}

function offlineDir(): StoredDirectory {
  return fileStore.ensureDir(INDEX_DIR);
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

// A ready record with no uri names no file, so it is as malformed as one whose uri is not a
// string: nothing downstream could play it, and reconcile re-queues the track from disk anyway.
// The fields a status does not carry are dropped here, so a stale uri cannot survive off disk.
function entryOfStatus(
  trackId: TrackId,
  status: PinnedStatus,
  uri?: string,
  version?: string,
): PinnedEntry | null {
  switch (status) {
    case 'ready':
      return uri === undefined ? null : readyEntry(trackId, uri, version);
    case 'failed':
      return failedEntry(trackId);
    case 'queued':
      return queuedEntry(trackId);
    case 'downloading':
      return downloadingEntry(trackId);
  }
}

// The index file is where a raw track id comes back off disk, so it is re-branded here: an entry
// whose id is not a valid TrackId is malformed, like one with an unknown status.
function narrowEntry(value: unknown): PinnedEntry | null {
  if (typeof value !== 'object' || value === null) return null;
  const { trackId: rawTrackId, status, uri, version } = value as Record<string, unknown>;
  if (typeof rawTrackId !== 'string' || !isPinnedStatus(status)) return null;
  if (uri !== undefined && typeof uri !== 'string') return null;
  if (version !== undefined && typeof version !== 'string') return null;
  const trackId = parseTrackId(rawTrackId);
  if (!trackId.ok) return null;
  return entryOfStatus(trackId.id, status, uri, version);
}

function narrowIndex(parsed: Record<string, unknown>): Record<string, PinnedEntry> {
  const entries: Record<string, PinnedEntry> = {};
  let dropped = 0;
  for (const [trackId, value] of Object.entries(parsed)) {
    // The map key, not the entry's own field, is what every later lookup and write uses, and it
    // arrives from disk unparsed. A key outside the id shape is as malformed as an unknown status.
    const entry = isSafeId(trackId) ? narrowEntry(value) : null;
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
