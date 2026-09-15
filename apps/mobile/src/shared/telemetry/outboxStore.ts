import { deviceFileStore, type FileStore, type StoredFile } from '@shared/files/fileStore';

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

let fileStore: FileStore = deviceFileStore;

/** Points the persisted outbox at `store`; with no argument, back at the device filesystem. */
export function setOutboxFileStore(store: FileStore = deviceFileStore): void {
  fileStore = store;
}

function outboxFile(): StoredFile {
  const dir = fileStore.openDirectory(OUTBOX_DIR);
  if (!dir.exists) dir.create();
  return dir.openFile(OUTBOX_FILE);
}

export function loadPersistedOutbox(): OutboxEntry[] {
  try {
    const file = outboxFile();
    if (!file.exists) return [];
    const parsed: unknown = JSON.parse(file.textSync());
    if (!Array.isArray(parsed)) return [];
    const kept = parsed.filter(isPersistedEntry);
    if (kept.length < parsed.length) {
      console.warn(
        `[telemetry] dropped ${parsed.length - kept.length} malformed outbox entries at load`,
      );
    }
    return kept;
  } catch {
    return [];
  }
}

export function persistOutbox(entries: readonly OutboxEntry[]): void {
  try {
    const file = outboxFile();
    if (entries.length === 0) {
      if (file.exists) file.delete();
      return;
    }
    file.write(JSON.stringify(entries));
  } catch {
    console.warn('[telemetry] failed to persist outbox; keeping in-memory only');
  }
}
