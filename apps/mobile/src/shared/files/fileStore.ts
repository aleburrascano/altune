export type StoredFile = {
  readonly uri: string;
  readonly exists: boolean;
  readonly size: number | null;
  textSync(): string;
  write(contents: string): void;
  delete(): void;
  moveTo(dest: StoredFile): void;
};

export type StoredDirectory = {
  readonly uri: string;
  readonly exists: boolean;
  create(): void;
  list(): readonly StoredFile[];
  openFile(name: string): StoredFile;
};

export type FileStore = {
  openDirectory(name: string): StoredDirectory;
  download(
    url: string,
    dest: StoredFile,
    signal: AbortSignal,
    onProgress?: (progress: { bytesWritten: number; totalBytes: number }) => void,
  ): Promise<string>;
  availableBytes(): number;
};

export type FileStoreSlot = {
  get(): FileStore;
  set(store?: FileStore): void;
  ensureDir(name: string): StoredDirectory;
};

function ensureDirectory(store: FileStore, name: string): StoredDirectory {
  const dir = store.openDirectory(name);
  if (!dir.exists) dir.create();
  return dir;
}

export function createFileStoreSlot(defaultStore: FileStore): FileStoreSlot {
  let store = defaultStore;
  return {
    get: () => store,
    set: (next = defaultStore) => {
      store = next;
    },
    ensureDir: (name) => ensureDirectory(store, name),
  };
}
