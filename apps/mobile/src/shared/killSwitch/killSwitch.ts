import { readDocument, writeDocumentAtomically } from '@shared/files/durableDocument';
import {
  createFileStoreSlot,
  type FileStore,
  type StoredDirectory,
} from '@shared/files/fileStore';

// Remote kill switches for the app's background loops (#955), for the detail screen's provider
// fetches (#1666) and for discover's search, suggest and history calls (#1685). Each reads its
// switch before it starts and before each further unit of work, and subscribes to be told when it
// flips. The switch document is the same shape the server-reported prefetch switch uses (#824): a
// boolean per loop, the last document seen wins, and a key the document does not send leaves that
// loop enabled. The last document applied is persisted, so a loop switched off stays off from the
// next cold start, before any refresh has returned.

export type KillSwitchLoop =
  | 'serverEvents'
  | 'telemetryFlush'
  | 'offlineDownloads'
  | 'detailEnrichment'
  | 'discovery';

type LoopFlags = Readonly<Record<KillSwitchLoop, boolean>>;

/** The wire key each loop's switch is read from. */
export const KILL_SWITCH_KEYS: Readonly<Record<KillSwitchLoop, string>> = {
  serverEvents: 'sse_enabled',
  telemetryFlush: 'telemetry_enabled',
  offlineDownloads: 'offline_downloads_enabled',
  detailEnrichment: 'detail_enrichment_enabled',
  discovery: 'discovery_enabled',
};

const LOOPS = Object.keys(KILL_SWITCH_KEYS) as KillSwitchLoop[];

const ALL_ENABLED: LoopFlags = {
  serverEvents: true,
  telemetryFlush: true,
  offlineDownloads: true,
  detailEnrichment: true,
  discovery: true,
};

const TAG = '[kill-switch]';
const SWITCH_DIR = 'kill-switch';
const SWITCH_FILE = 'switches.json';

const fileStore = createFileStoreSlot();
let flags: LoopFlags = ALL_ENABLED;
let restored = false;
const listeners = new Set<(loop: KillSwitchLoop, enabled: boolean) => void>();

// Reads never create the directory: a device that never saw a switch document keeps no trace of one.
function openSwitchDir(): StoredDirectory {
  return fileStore.get().openDirectory(SWITCH_DIR);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** Reads a switch document; null when it is not an object at all, so it is ignored. */
function parseSwitches(document: unknown): LoopFlags | null {
  if (!isPlainObject(document)) return null;
  const parsed = { ...ALL_ENABLED };
  for (const loop of LOOPS) parsed[loop] = document[KILL_SWITCH_KEYS[loop]] !== false;
  return parsed;
}

function toDocument(next: LoopFlags): Record<string, unknown> {
  const document: Record<string, unknown> = { schemaVersion: 1 };
  for (const loop of LOOPS) document[KILL_SWITCH_KEYS[loop]] = next[loop];
  return document;
}

function ensureRestored(): void {
  if (restored) return;
  restored = true;
  const read = readDocument(TAG, SWITCH_FILE, openSwitchDir);
  if (read.status !== 'read') return;
  flags = parseSwitches(read.document) ?? ALL_ENABLED;
}

function persist(next: LoopFlags): void {
  try {
    writeDocumentAtomically(
      fileStore.ensureDir(SWITCH_DIR),
      SWITCH_FILE,
      JSON.stringify(toDocument(next)),
    );
  } catch {
    console.warn(`${TAG} failed to persist switches; keeping them in memory only`);
  }
}

/** Whether `loop` may start or continue its work. */
export function isLoopEnabled(loop: KillSwitchLoop): boolean {
  ensureRestored();
  return flags[loop];
}

/** Calls `listener` each time one loop's switch flips; returns the unsubscribe. */
export function onKillSwitchChange(
  listener: (loop: KillSwitchLoop, enabled: boolean) => void,
): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Applies a switch document fetched from the remote source. Anything but an object is ignored so a
 * garbled response never re-enables a loop that was switched off.
 */
export function applyKillSwitches(document: unknown): void {
  ensureRestored();
  const next = parseSwitches(document);
  if (next === null) {
    console.warn(`${TAG} ignored a switch document that is not an object`);
    return;
  }
  const flipped = LOOPS.filter((loop) => next[loop] !== flags[loop]);
  if (flipped.length === 0) return;
  flags = next;
  persist(next);
  for (const loop of flipped) {
    console.warn(`${TAG} ${loop} ${next[loop] ? 'enabled' : 'disabled'} remotely`);
    for (const listener of [...listeners]) listener(loop, next[loop]);
  }
}

/** Points the persisted switches at `store` (default: the device) and forgets the in-memory state. */
export function setKillSwitchFileStore(store?: FileStore): void {
  fileStore.set(store);
  flags = ALL_ENABLED;
  restored = false;
}
