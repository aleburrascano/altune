import type { StoredDirectory, StoredFile } from './fileStore';

type OpenDir = () => StoredDirectory;

function tempName(name: string): string {
  return `${name}.tmp`;
}

export function writeDocumentAtomically(
  dir: StoredDirectory,
  name: string,
  contents: string,
): void {
  const temp = dir.openFile(tempName(name));
  temp.write(contents);
  temp.moveTo(dir.openFile(name));
}

export function deleteDocument(dir: StoredDirectory, name: string): void {
  for (const file of [dir.openFile(tempName(name)), dir.openFile(name)]) {
    if (file.exists) file.delete();
  }
}

export type ReadDocumentResult =
  { status: 'absent' } | { status: 'unreadable' } | { status: 'read'; document: unknown };

const UNREADABLE: ReadDocumentResult = { status: 'unreadable' };

function errorName(error: unknown): string {
  return error instanceof Error ? error.name : typeof error;
}

function fileName(file: StoredFile): string {
  return file.uri.slice(file.uri.lastIndexOf('/') + 1);
}

function candidates(tag: string, name: string, openDir: OpenDir): StoredFile[] | null {
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

function firstReadable(tag: string, name: string, files: StoredFile[]): ReadDocumentResult {
  for (const [i, file] of files.entries()) {
    const next = i + 1 < files.length ? `trying ${tempName(name)}` : 'treating it as empty';
    const result = parseFile(tag, file, next);
    if (result.status === 'read') return result;
  }
  return UNREADABLE;
}

export function readDocument(tag: string, name: string, openDir: OpenDir): ReadDocumentResult {
  const files = candidates(tag, name, openDir);
  if (files === null) return UNREADABLE;
  if (files.length === 0) return { status: 'absent' };
  return firstReadable(tag, name, files);
}

export type Migration = (document: unknown) => unknown;

export type SchemaSpec = {
  current: number;
  migrations: readonly Migration[];
};

export function schemaVersionOf(document: unknown): number {
  if (typeof document !== 'object' || document === null || Array.isArray(document)) return 0;
  const version = (document as Record<string, unknown>)['schemaVersion'];
  return typeof version === 'number' && Number.isInteger(version) && version >= 0 ? version : 0;
}

export type MigrateResult =
  | { status: 'current'; document: unknown }
  | { status: 'newer'; version: number; document: unknown };

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

export type ReadEntriesResult =
  { status: 'absent' | 'unreadable' } | { status: 'read'; entries: unknown };

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
  return { status: 'read', entries: (migrated.document as { entries?: unknown }).entries };
}
