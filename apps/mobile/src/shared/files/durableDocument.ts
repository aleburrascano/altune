import type { StoredDirectory, StoredFile } from './fileStore';

// A small JSON document persisted so a killed process cannot leave it half-written, stamped with a
// schema version so an older shape is migrated forward instead of failing validation and vanishing.
// Used by the pinned-download index and the telemetry outbox.

function tempName(name: string): string {
  return `${name}.tmp`;
}

/**
 * Writes `contents` to a temp file beside `name`, then renames it over `name`, so the committed
 * file only ever holds a complete write.
 */
export function writeDocumentAtomically(dir: StoredDirectory, name: string, contents: string): void {
  const temp = dir.openFile(tempName(name));
  temp.write(contents);
  temp.moveTo(dir.openFile(name));
}

/** Removes the committed document and any temp write left beside it. */
export function deleteDocument(dir: StoredDirectory, name: string): void {
  for (const file of [dir.openFile(tempName(name)), dir.openFile(name)]) {
    if (file.exists) file.delete();
  }
}

export type ReadDocumentResult =
  | { status: 'absent' }
  | { status: 'unreadable' }
  | { status: 'read'; document: unknown };

const UNREADABLE: ReadDocumentResult = { status: 'unreadable' };

// Warnings name the file and the error's name only: an error message can quote file contents,
// which may be user data.
function errorName(error: unknown): string {
  return error instanceof Error ? error.name : typeof error;
}

function fileName(file: StoredFile): string {
  return file.uri.slice(file.uri.lastIndexOf('/') + 1);
}

// The committed file, then its temp file. A temp file only exists while a write is unfinished, so
// it is never older than the committed one: when a rename that replaces its destination is not
// atomic (iOS removes, then moves) a kill mid-rename leaves the complete write only there.
function candidates(tag: string, name: string, openDir: () => StoredDirectory): StoredFile[] | null {
  try {
    const dir = openDir();
    return [dir.openFile(name), dir.openFile(tempName(name))].filter((file) => file.exists);
  } catch (error) {
    console.warn(`${tag} could not read ${name} (${errorName(error)}); treating it as empty`);
    return null;
  }
}

function parseFile(tag: string, file: StoredFile, next: string): ReadDocumentResult {
  let failure = 'could not be read';
  try {
    const text = file.textSync();
    failure = 'is corrupt';
    return { status: 'read', document: JSON.parse(text) as unknown };
  } catch (error) {
    console.warn(`${tag} ${fileName(file)} ${failure} (${errorName(error)}); ${next}`);
    return UNREADABLE;
  }
}

/**
 * Reads and parses the document `name` in the directory `openDir` returns, falling back to an
 * unfinished write's temp file when the committed file is missing or damaged. Every read or parse
 * failure is logged under `tag`.
 */
export function readDocument(tag: string, name: string, openDir: () => StoredDirectory): ReadDocumentResult {
  const files = candidates(tag, name, openDir);
  if (files === null) return UNREADABLE;
  if (files.length === 0) return { status: 'absent' };
  for (const [i, file] of files.entries()) {
    const next = i + 1 < files.length ? `trying ${tempName(name)}` : 'treating it as empty';
    const result = parseFile(tag, file, next);
    if (result.status === 'read') return result;
  }
  return UNREADABLE;
}

/** Upgrades a document written at one schema version into the shape of the next. */
export type Migration = (document: unknown) => unknown;

export type SchemaSpec = {
  /** The version this build writes. */
  current: number;
  /**
   * `migrations[v]` upgrades a version-`v` document to version `v + 1`. Version 0 is the shape
   * written before documents carried a `schemaVersion`.
   */
  migrations: readonly Migration[];
};

/** The stamped version of a parsed document, or 0 when it predates versioning. */
export function schemaVersionOf(document: unknown): number {
  if (typeof document !== 'object' || document === null || Array.isArray(document)) return 0;
  const version = (document as Record<string, unknown>)['schemaVersion'];
  return typeof version === 'number' && Number.isInteger(version) && version >= 0 ? version : 0;
}

export type MigrateResult =
  | { status: 'current'; document: unknown }
  | { status: 'newer'; version: number; document: unknown };

/**
 * Runs every migration between the document's version and `spec.current`. A document from a newer
 * build cannot be migrated down, so it is returned as-is for the caller to read best-effort.
 * Throws when `spec` lacks a step, which is a programming error.
 */
export function migrateDocument(document: unknown, spec: SchemaSpec): MigrateResult {
  const version = schemaVersionOf(document);
  if (version > spec.current) return { status: 'newer', version, document };
  let migrated = document;
  for (let v = version; v < spec.current; v += 1) {
    const migrate = spec.migrations[v];
    if (migrate === undefined) throw new Error(`no migration from schema version ${v}`);
    migrated = migrate(migrated);
  }
  return { status: 'current', document: migrated };
}

export type ReadEntriesResult = { status: 'absent' | 'unreadable' } | { status: 'read'; entries: unknown };

/**
 * Reads a versioned `{ schemaVersion, entries }` document, migrated to `spec.current`, and returns
 * its unvalidated `entries`. A document from a newer build is logged and read best-effort.
 */
export function readVersionedEntries(
  tag: string,
  name: string,
  openDir: () => StoredDirectory,
  spec: SchemaSpec,
): ReadEntriesResult {
  const read = readDocument(tag, name, openDir);
  if (read.status !== 'read') return read;
  const migrated = migrateDocument(read.document, spec);
  if (migrated.status === 'newer') {
    console.warn(
      `${tag} ${name} has schema version ${migrated.version}, newer than ${spec.current}; reading what this version understands`,
    );
  }
  // A migrated or version-stamped document is always an object.
  return { status: 'read', entries: (migrated.document as { entries?: unknown }).entries };
}
