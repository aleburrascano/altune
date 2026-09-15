import { isSafeId } from '@shared/api-client/ids';
import {
  deviceFileStore,
  type FileStore,
  type StoredDirectory,
  type StoredFile,
} from '@shared/files/fileStore';

const PINNED_SUBDIR = 'offline-audio';

let fileStore: FileStore = deviceFileStore;

/** Points pinned-file reads and writes at `store`; with no argument, back at the device filesystem. */
export function setPinnedFileStore(store: FileStore = deviceFileStore): void {
  fileStore = store;
}

export function pinnedDir(): StoredDirectory {
  const dir = fileStore.openDirectory(PINNED_SUBDIR);
  if (!dir.exists) dir.create();
  return dir;
}

function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

export function extFromUrl(url: string): string {
  const path = url.split('?')[0] ?? '';
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash && dot < path.length - 1 ? path.slice(dot) : '.mp3';
}

function pinnedFilesOnDisk(): readonly StoredFile[] {
  try {
    return pinnedDir().list();
  } catch {
    return [];
  }
}

export function pinnedDirReadable(): boolean {
  try {
    pinnedDir().list();
    return true;
  } catch {
    return false;
  }
}

// A pinned file is named after its track id, so an id outside the safe shape is refused before it
// becomes a path segment: a `/` or `..` could escape the pinned directory, and an empty id would
// prefix-match (and so find or delete) some other track's file. No file can exist for such an id,
// so lookups report none and deletes have nothing to remove; a download throws.
export function findPinned(trackId: string): StoredFile | null {
  if (!isSafeId(trackId)) return null;
  for (const file of pinnedFilesOnDisk()) {
    if (baseName(file.uri).startsWith(`${trackId}.`)) return file;
  }
  return null;
}

// A failed delete (e.g. an OS-locked file) must not abort the pass, but it is
// reported so callers keep indexing the bytes that are still on disk.
function tryDelete(file: StoredFile): boolean {
  try {
    file.delete();
    return true;
  } catch {
    console.warn(`[offline] failed to delete pinned file ${baseName(file.uri)}`);
    return false;
  }
}

/** Returns false only when the track's file exists and could not be deleted. */
export function deletePinned(trackId: string): boolean {
  const file = findPinned(trackId);
  if (file === null) return true;
  return tryDelete(file);
}

/** Deletes every pinned file, continuing past failures; returns false if any remain. */
export function deleteAllPinned(): boolean {
  let allDeleted = true;
  for (const file of pinnedFilesOnDisk()) allDeleted = tryDelete(file) && allDeleted;
  return allDeleted;
}

export function pinnedBytes(): number {
  let total = 0;
  for (const file of pinnedFilesOnDisk()) total += file.size ?? 0;
  return total;
}

export async function downloadPinned(trackId: string, url: string): Promise<string> {
  if (!isSafeId(trackId)) throw new Error('[offline] refused to pin an invalid track id');
  const dest = pinnedDir().openFile(`${trackId}${extFromUrl(url)}`);
  return fileStore.download(url, dest);
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`;
}
