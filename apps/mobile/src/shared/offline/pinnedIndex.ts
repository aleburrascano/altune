import { Directory, File, Paths } from 'expo-file-system';

// Persistence of the pinned-download index and its owner marker: the on-disk
// shape, how a loaded file is narrowed back into entries, and best-effort writes.

export type PinnedStatus = 'queued' | 'downloading' | 'ready' | 'failed';

export type PinnedEntry = {
  trackId: string;
  status: PinnedStatus;
  uri?: string;
  version?: string;
};

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';
const OWNER_FILE = 'pinned-owner';

function offlineFile(name: string): File {
  const dir = new Directory(Paths.document, INDEX_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, name);
}

function indexFile(): File {
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
