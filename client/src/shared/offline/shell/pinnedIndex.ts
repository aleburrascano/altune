import {
  defaultFileStore,
  readVersionedEntries,
  writeDocumentAtomically,
  type SchemaSpec,
  createFileStoreSlot,
  type FileStore,
  type StoredDirectory,
  type StoredFile,
} from '@shared/files';
import { warn } from '@shared/log';
import { isPlainObject, narrowIndex, type PinnedEntry } from '@shared/offline/core/pinnedEntry';

const INDEX_DIR = 'offline';
const INDEX_FILE = 'pinned.json';
const OWNER_FILE = 'pinned-owner';

const fileStore = createFileStoreSlot(defaultFileStore);

export function setPinnedIndexFileStore(store?: FileStore): void {
  fileStore.set(store);
}

function offlineDir(): StoredDirectory {
  return fileStore.ensureDir(INDEX_DIR);
}

function offlineFile(name: string): StoredFile {
  return offlineDir().openFile(name);
}

const INDEX_SCHEMA_VERSION = 1;

const INDEX_SCHEMA: SchemaSpec = {
  current: INDEX_SCHEMA_VERSION,
  migrations: [(bareMap) => ({ schemaVersion: 1, entries: bareMap })],
};

export function loadIndex(): Record<string, PinnedEntry> {
  const read = readVersionedEntries('[offline]', INDEX_FILE, offlineDir, INDEX_SCHEMA);
  if (read.status !== 'read') return {};
  if (!isPlainObject(read.entries)) {
    warn(`[offline] ${INDEX_FILE} is not a pinned index; treating it as empty`);
    return {};
  }
  const { entries, dropped } = narrowIndex(read.entries);
  if (dropped > 0) warn(`[offline] dropped ${dropped} malformed pinned entries at load`);
  return entries;
}

const INDEX_WRITE_DELAY_MS = 2_000;

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
  } catch (error) {
    warn('[offline] failed to persist pinned index; keeping in-memory only', {
      file: INDEX_FILE,
      entryCount: Object.keys(entries).length,
      error,
    });
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
  } catch (error) {
    warn('[offline] failed to persist pinned owner; downloads will be cleared next launch', {
      file: OWNER_FILE,
      userId,
      error,
    });
  }
}
