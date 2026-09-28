import { readDocument, writeDocumentAtomically } from '@shared/files/durableDocument';
import {
  createFileStoreSlot,
  defaultFileStore,
  type FileStore,
  type StoredDirectory,
} from '@shared/files/fileStore';

export type KillSwitchLoop =
  'serverEvents' | 'telemetryFlush' | 'offlineDownloads' | 'detailEnrichment' | 'discovery';

type LoopFlags = Readonly<Record<KillSwitchLoop, boolean>>;

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

const fileStore = createFileStoreSlot(defaultFileStore);
let flags: LoopFlags = ALL_ENABLED;
let restored = false;
const listeners = new Set<(loop: KillSwitchLoop, enabled: boolean) => void>();

function openSwitchDir(): StoredDirectory {
  return fileStore.get().openDirectory(SWITCH_DIR);
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

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

export function isLoopEnabled(loop: KillSwitchLoop): boolean {
  ensureRestored();
  return flags[loop];
}

export function onKillSwitchChange(
  listener: (loop: KillSwitchLoop, enabled: boolean) => void,
): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

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

export function setKillSwitchFileStore(store?: FileStore): void {
  fileStore.set(store);
  flags = ALL_ENABLED;
  restored = false;
}
