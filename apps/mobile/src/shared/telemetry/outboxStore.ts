import {
  deleteDocument,
  readVersionedEntries,
  writeDocumentAtomically,
  type SchemaSpec,
} from '@shared/files/durableDocument';
import {
  createFileStoreSlot,
  type FileStore,
  type StoredDirectory,
} from '@shared/files/fileStore';

import type { OutboxEntry } from './outbox';
import type { DiscoveryEventType } from './recordEvent';

const OUTBOX_DIR = 'telemetry';
const OUTBOX_FILE = 'critical-outbox.json';

const DISCOVERY_EVENT_TYPES: Record<DiscoveryEventType, true> = {
  results_shown: true,
  result_clicked: true,
  play: true,
  skip: true,
  completed: true,
  library_add: true,
  wrong_album: true,
  search_failed: true,
  search_degraded: true,
  playback_health: true,
  detail_health: true,
  acquisition_ui: true,
  client_error: true,
};

function isDiscoveryEventType(value: unknown): value is DiscoveryEventType {
  return typeof value === 'string' && DISCOVERY_EVENT_TYPES[value as DiscoveryEventType] === true;
}

function isPersistedEntry(e: unknown): e is OutboxEntry {
  if (typeof e !== 'object' || e === null) return false;
  const record = e as Record<string, unknown>;
  if (!isDiscoveryEventType(record['type'])) return false;
  if (typeof record['event_id'] !== 'string' || record['event_id'].length === 0) return false;
  if (record['owner_user_id'] !== undefined && typeof record['owner_user_id'] !== 'string') {
    return false;
  }
  return typeof record['client_occurred_at'] === 'string';
}

const fileStore = createFileStoreSlot();

/** Points the persisted outbox at `store`; with no argument, back at the device filesystem. */
export function setOutboxFileStore(store?: FileStore): void {
  fileStore.set(store);
}

function outboxDir(): StoredDirectory {
  return fileStore.ensureDir(OUTBOX_DIR);
}

/** The schema version `persistOutbox` stamps on the outbox file. */
export const OUTBOX_SCHEMA_VERSION = 1;

// Version 0 is the bare array of entries written before the outbox carried a version.
const OUTBOX_SCHEMA: SchemaSpec = {
  current: OUTBOX_SCHEMA_VERSION,
  migrations: [(bareArray) => ({ schemaVersion: 1, entries: bareArray })],
};

function keepPersistedEntries(entries: readonly unknown[]): OutboxEntry[] {
  const kept = entries.filter(isPersistedEntry);
  const dropped = entries.length - kept.length;
  if (dropped > 0) console.warn(`[telemetry] dropped ${dropped} malformed outbox entries at load`);
  return kept;
}

export function loadPersistedOutbox(): OutboxEntry[] {
  const read = readVersionedEntries('[telemetry]', OUTBOX_FILE, outboxDir, OUTBOX_SCHEMA);
  if (read.status !== 'read') return [];
  if (!Array.isArray(read.entries)) {
    console.warn(`[telemetry] ${OUTBOX_FILE} is not an outbox; treating it as empty`);
    return [];
  }
  return keepPersistedEntries(read.entries);
}

export function persistOutbox(entries: readonly OutboxEntry[]): void {
  try {
    const dir = outboxDir();
    if (entries.length === 0) {
      deleteDocument(dir, OUTBOX_FILE);
      return;
    }
    const document = { schemaVersion: OUTBOX_SCHEMA_VERSION, entries };
    writeDocumentAtomically(dir, OUTBOX_FILE, JSON.stringify(document));
  } catch {
    console.warn('[telemetry] failed to persist outbox; keeping in-memory only');
  }
}
