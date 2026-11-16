export { defaultFileStore, setDeviceFileStore } from '@shared/files/defaultFileStore';
export {
  deleteDocument,
  readDocument,
  readVersionedEntries,
  writeDocumentAtomically,
} from '@shared/files/durableDocument';
export type { SchemaSpec } from '@shared/files/durableDocument';
export { createFileStoreSlot } from '@shared/files/fileStore';
export type { FileStore, StoredDirectory, StoredFile } from '@shared/files/fileStore';
export type { JsonObject, JsonValue } from '@shared/files/json';
export { extFromUrl } from '@shared/files/urlExtension';
export { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
export type { MemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
