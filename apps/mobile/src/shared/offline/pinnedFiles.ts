import { NetworkError } from '@shared/errors';
import { startDeadline } from '@shared/deadline/deadline';
import { isSafeId, type TrackId } from '@shared/api-client/ids';
import {
  createFileStoreSlot,
  type FileStore,
  type StoredDirectory,
  type StoredFile,
} from '@shared/files/fileStore';

const PINNED_SUBDIR = 'offline-audio';

export const PIN_DOWNLOAD_TIMEOUT_MS = 120_000;
export const MAX_PINNED_BYTES = 8 * 1024 ** 3;
export const MIN_FREE_BYTES = 512 * 1024 ** 2;

const fileStore = createFileStoreSlot();

export function setPinnedFileStore(store?: FileStore): void {
  fileStore.set(store);
  forgetRunningTotal();
}

export function pinnedDir(): StoredDirectory {
  return fileStore.ensureDir(PINNED_SUBDIR);
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

function trackIdOfFile(file: StoredFile): string | null {
  const [trackId, extension, ...beyondOneExtension] = baseName(file.uri).split('.');
  const isTrackIdWithOneExtension = extension !== undefined && beyondOneExtension.length === 0;
  return isTrackIdWithOneExtension ? (trackId ?? null) : null;
}

const UNFINISHED_SUFFIX = '.tmp';

function unfinishedName(pinnedName: string): string {
  return `${pinnedName}${UNFINISHED_SUFFIX}`;
}

function isUnfinishedDownload(file: StoredFile): boolean {
  return baseName(file.uri).endsWith(UNFINISHED_SUFFIX);
}

export function pinnedFilesByTrackId(): ReadonlyMap<string, StoredFile> | null {
  let files: readonly StoredFile[];
  try {
    files = pinnedDir().list();
  } catch {
    return null;
  }
  const byTrackId = new Map<string, StoredFile>();
  for (const file of files) {
    const trackId = trackIdOfFile(file);
    if (trackId !== null && isSafeId(trackId) && !byTrackId.has(trackId)) {
      byTrackId.set(trackId, file);
    }
  }
  return byTrackId;
}

export function findPinned(trackId: TrackId): StoredFile | null {
  if (!isSafeId(trackId)) return null;
  for (const file of pinnedFilesOnDisk()) {
    if (trackIdOfFile(file) === trackId) return file;
  }
  return null;
}

function measurePinnedBytes(): number {
  let total = 0;
  for (const file of pinnedFilesOnDisk()) total += file.size ?? 0;
  return total;
}

let cachedBytes: number | null = null;

function forgetRunningTotal(): void {
  cachedBytes = null;
}

function countWrittenBytes(file: StoredFile): void {
  if (cachedBytes === null) return;
  if (file.size === null) forgetRunningTotal();
  else cachedBytes += file.size;
}

function countDeletedBytes(bytes: number): void {
  if (cachedBytes === null) return;
  cachedBytes -= bytes;
  if (cachedBytes < 0) forgetRunningTotal();
}

export function pinnedBytes(): number {
  return cachedBytes ?? measurePinnedBytes();
}

export async function withPinnedBytesCached<T>(pass: () => Promise<T>): Promise<T> {
  cachedBytes = measurePinnedBytes();
  try {
    return await pass();
  } finally {
    forgetRunningTotal();
  }
}

function tryDelete(file: StoredFile): boolean {
  try {
    file.delete();
    return true;
  } catch {
    console.warn(`[offline] failed to delete pinned file ${baseName(file.uri)}`);
    return false;
  }
}

function tryDeleteCounted(file: StoredFile): boolean {
  const bytesOnDisk = file.size ?? 0;
  if (!tryDelete(file)) return false;
  countDeletedBytes(bytesOnDisk);
  return true;
}

export function deletePinned(trackId: TrackId): boolean {
  const file = findPinned(trackId);
  if (file === null) return true;
  return tryDeleteCounted(file);
}

export function deletePinnedMany(trackIds: readonly TrackId[]): ReadonlySet<TrackId> {
  const onDisk = pinnedFilesByTrackId();
  if (onDisk === null) return new Set(trackIds);
  const stillOnDisk = new Set<TrackId>();
  for (const trackId of trackIds) {
    const file = onDisk.get(trackId);
    if (file !== undefined && !tryDeleteCounted(file)) stillOnDisk.add(trackId);
  }
  return stillOnDisk;
}

export function deleteAbandonedDownloads(): void {
  for (const file of pinnedFilesOnDisk()) {
    if (isUnfinishedDownload(file)) tryDeleteCounted(file);
  }
}

export function deleteAllPinned(): boolean {
  let allDeleted = true;
  for (const file of pinnedFilesOnDisk()) allDeleted = tryDeleteCounted(file) && allDeleted;
  return allDeleted;
}

function freeSpaceBelowReserve(): boolean {
  try {
    return fileStore.get().availableBytes() < MIN_FREE_BYTES;
  } catch {
    return false;
  }
}

export function pinStorageFull(): boolean {
  return pinnedBytes() >= MAX_PINNED_BYTES || freeSpaceBelowReserve();
}

export function unsignedUrl(url: string | undefined): string | undefined {
  return url?.split('?')[0];
}

function rejectOnAbort(signal: AbortSignal): Promise<never> {
  return new Promise((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
  });
}

export async function downloadPinned(trackId: TrackId, url: string): Promise<string> {
  if (!isSafeId(trackId)) throw new Error('[offline] refused to pin an invalid track id');
  const dir = pinnedDir();
  const pinnedName = `${trackId}${extFromUrl(url)}`;
  const unfinished = dir.openFile(unfinishedName(pinnedName));
  const deadline = startDeadline(undefined, PIN_DOWNLOAD_TIMEOUT_MS);
  try {
    await Promise.race([
      fileStore.get().download(url, unfinished, deadline.signal),
      rejectOnAbort(deadline.signal),
    ]).catch((error: unknown) => {
      throw deadline.expired()
        ? new NetworkError(
            'timeout',
            `[offline] download timed out after ${PIN_DOWNLOAD_TIMEOUT_MS}ms`,
          )
        : new NetworkError('transport', `[offline] download failed: ${String(error)}`);
    });
    const pinned = dir.openFile(pinnedName);
    unfinished.moveTo(pinned);
    countWrittenBytes(pinned);
    return pinned.uri;
  } catch (error) {
    if (unfinished.exists && !tryDelete(unfinished)) forgetRunningTotal();
    throw error;
  } finally {
    deadline.release();
  }
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
