import { Directory, File, Paths } from 'expo-file-system';

// The filesystem port the persisted stores (pinned audio + index, telemetry outbox) write through.
// Each consumer binds `deviceFileStore` by default and exposes a setter, so one test can hand one
// module a scoped fake instead of reconfiguring the suite-wide expo-file-system jest mock.
// The contract every implementation must satisfy is pinned by __tests__/fileStore.contract.test.ts.

/** A file handle. Opening one never touches the disk; the file need not exist yet. */
export type StoredFile = {
  readonly uri: string;
  readonly exists: boolean;
  /** Byte size, or null when the file does not exist. */
  readonly size: number | null;
  /** Throws when the file does not exist or cannot be read. */
  textSync(): string;
  /** Creates or replaces the file's contents. */
  write(contents: string): void;
  delete(): void;
};

/** A directory handle. Opening one never touches the disk; the directory need not exist yet. */
export type StoredDirectory = {
  readonly uri: string;
  readonly exists: boolean;
  /** Creates the directory and any missing parents. */
  create(): void;
  /** The files directly inside the directory (subdirectories are skipped); throws if unreadable. */
  list(): readonly StoredFile[];
  openFile(name: string): StoredFile;
};

export type FileStore = {
  /** Opens a directory under the app's document root, which the OS never evicts. */
  openDirectory(name: string): StoredDirectory;
  /** Downloads `url` into `dest`, replacing any existing file; resolves to the written file's uri. */
  download(url: string, dest: StoredFile): Promise<string>;
};

function directoryHandle(dir: Directory): StoredDirectory {
  return {
    uri: dir.uri,
    get exists() {
      return dir.exists;
    },
    create: () => dir.create({ intermediates: true }),
    list: () => dir.list().filter((entry): entry is File => entry instanceof File),
    openFile: (name) => new File(dir, name),
  };
}

/** The real on-device filesystem, backed by expo-file-system. */
export const deviceFileStore: FileStore = {
  openDirectory: (name) => directoryHandle(new Directory(Paths.document, name)),
  download: async (url, dest) => {
    const file = await File.downloadFileAsync(url, new File(dest.uri), { idempotent: true });
    return file.uri;
  },
};
