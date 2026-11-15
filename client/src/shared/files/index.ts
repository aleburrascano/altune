export { defaultFileStore } from '@shared/files/defaultFileStore';
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
export { deviceFileStore, fileHandle } from '@shared/files/native/deviceFileStore';
export { extFromUrl } from '@shared/files/urlExtension';
