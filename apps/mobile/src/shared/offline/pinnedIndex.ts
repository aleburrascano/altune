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

export type PinnedEntry =
  | { trackId: TrackId; status: 'ready'; uri: string; version?: string }
  | { trackId: TrackId; status: 'failed' }
  | { trackId: TrackId; status: 'queued' | 'downloading' };

export type PinnedStatus = PinnedEntry['status'];

const changedIdsByEntries = new WeakMap<Record<string, PinnedEntry>, readonly string[]>();

export function tagChangedEntries(
  entries: Record<string, PinnedEntry>,
  ids: readonly string[],
): void {
  changedIdsByEntries.set(entries, ids);
}

export function changedEntryIds(
  entries: Record<string, PinnedEntry>,
): readonly string[] | undefined {
  return changedIdsByEntries.get(entries);
}

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

export function setPinnedIndexFileStore(store?: FileStore): void {
  fileStore.set(store);
}

function offlineDir(): StoredDirectory {
  return fileStore.ensureDir(INDEX_DIR);
}

function offlineFile(name: string): StoredFile {
  return offlineDir().openFile(name);
}

export const INDEX_SCHEMA_VERSION = 1;

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

export const INDEX_WRITE_DELAY_MS = 2_000;

let pendingEntries: Record<string, PinnedEntry> | null = null;
let pendingTimer: ReturnType<typeof setTimeout> | null = null;

function cancelPendingSave(): void {
  if (pendingTimer !== null) clearTimeout(pendingTimer);
  pendingTimer = null;
  pendingEntries = null;
}

export function scheduleSaveIndex(entries: Record<string, PinnedEntry>): void {
  pendingEntries = entries;
  pendingTimer ??= setTimeout(flushIndex, INDEX_WRITE_DELAY_MS);
}

export function flushIndex(): void {
  if (pendingEntries !== null) saveIndex(pendingEntries);
}

export function saveIndex(entries: Record<string, PinnedEntry>): void {
  cancelPendingSave();
  try {
    const document = { schemaVersion: INDEX_SCHEMA_VERSION, entries };
    writeDocumentAtomically(offlineDir(), INDEX_FILE, JSON.stringify(document));
  } catch {
    console.warn('[offline] failed to persist pinned index; keeping in-memory only');
  }
}

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
