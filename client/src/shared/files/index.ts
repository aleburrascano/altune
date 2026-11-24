export { defaultFileStore, setDeviceFileStore } from '@shared/files/shell/defaultFileStore';
export {
  deleteDocument,
  readDocument,
  readVersionedEntries,
  writeDocumentAtomically,
} from '@shared/files/shell/durableDocument';
export type { SchemaSpec } from '@shared/files/shell/durableDocument';
export { createFileStoreSlot } from '@shared/files/shell/fileStore';
export type { FileStore, StoredDirectory, StoredFile } from '@shared/files/shell/fileStore';
export type { JsonObject } from '@shared/files/core/json';
export { extFromUrl } from '@shared/files/core/urlExtension';
