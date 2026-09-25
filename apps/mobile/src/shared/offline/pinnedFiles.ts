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

/** How long one track's download may run before it is aborted and marked failed. */
export const PIN_DOWNLOAD_TIMEOUT_MS = 120_000;
/** Pinning stops once the downloaded audio reaches this many bytes. */
export const MAX_PINNED_BYTES = 8 * 1024 ** 3;
/** Pinning stops once the device has less free space than this left. */
export const MIN_FREE_BYTES = 512 * 1024 ** 2;

const fileStore = createFileStoreSlot();

/** Points pinned-file reads and writes at `store`; with no argument, back at the device filesystem. */
export function setPinnedFileStore(store?: FileStore): void {
  fileStore.set(store);
  // A byte total measured on one filesystem says nothing about the next one's.
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

/**
 * Every pinned file keyed by the track id it is named after, from a single directory listing, so
 * a caller checking many tracks pays one listing instead of one per track. The first listed file
 * wins for an id, as with findPinned. Null when the directory cannot be read, which callers must
 * not mistake for "no files".
 */
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

// A pinned file is named after its track id, so an id outside the safe shape is refused before it
// becomes a path segment: a `/` or `..` could escape the pinned directory, and an empty id would
// prefix-match (and so find or delete) some other track's file. No file can exist for such an id,
// so lookups report none and deletes have nothing to remove; a download throws. The shape is
// re-checked despite the brand because a cast can still smuggle a raw string in, as in idPathSegment.
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

// The byte total of the drain pass in progress, or null when no pass is open and every read
// measures the directory instead.
let cachedBytes: number | null = null;

function forgetRunningTotal(): void {
  cachedBytes = null;
}

// A file with no size after a successful write would leave the total under-counting the cap, so
// the running total is dropped and the rest of the pass measures. Counting a replaced file's
// bytes twice can only refuse a download early, never overrun the cap, so it is left alone.
function countWrittenBytes(file: StoredFile): void {
  if (cachedBytes === null) return;
  if (file.size === null) forgetRunningTotal();
  else cachedBytes += file.size;
}

function countDeletedBytes(bytes: number): void {
  if (cachedBytes === null) return;
  cachedBytes -= bytes;
  // Below zero the total has lost bytes it never counted, so it no longer describes the disk.
  if (cachedBytes < 0) forgetRunningTotal();
}

export function pinnedBytes(): number {
  return cachedBytes ?? measurePinnedBytes();
}

/**
 * Runs `pass` against one measurement of the pinned byte total, which the writes and deletes made
 * during `pass` keep current, so a drain of n tracks pays one directory listing rather than n. The
 * total is dropped when `pass` ends, so the next one measures what is on disk by then.
 */
export async function withPinnedBytesCached<T>(pass: () => Promise<T>): Promise<T> {
  cachedBytes = measurePinnedBytes();
  try {
    return await pass();
  } finally {
    forgetRunningTotal();
  }
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

// Removing a file the running total counts takes its bytes with it.
function tryDeleteCounted(file: StoredFile): boolean {
  const bytesOnDisk = file.size ?? 0;
  if (!tryDelete(file)) return false;
  countDeletedBytes(bytesOnDisk);
  return true;
}

/** Returns false only when the track's file exists and could not be deleted. */
export function deletePinned(trackId: TrackId): boolean {
  const file = findPinned(trackId);
  if (file === null) return true;
  return tryDeleteCounted(file);
}

/**
 * Deletes the pinned files of `trackIds` from a single directory listing, so removing n downloads
 * costs one listing rather than n. Returns the ids whose file is still on disk — which, when the
 * directory cannot be listed at all, is every id asked for, since none of them can have been deleted.
 */
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

/** Deletes every pinned file, continuing past failures; returns false if any remain. */
export function deleteAllPinned(): boolean {
  let allDeleted = true;
  for (const file of pinnedFilesOnDisk()) allDeleted = tryDeleteCounted(file) && allDeleted;
  return allDeleted;
}

// A platform that cannot report free space does not block pinning; the pinned-bytes cap still holds.
function freeSpaceBelowReserve(): boolean {
  try {
    return fileStore.get().availableBytes() < MIN_FREE_BYTES;
  } catch {
    return false;
  }
}

/** True when another download would push past the pinned-bytes cap or eat the free-space reserve. */
export function pinStorageFull(): boolean {
  return pinnedBytes() >= MAX_PINNED_BYTES || freeSpaceBelowReserve();
}

/** The url without its query, so a signed url's credentials never reach a log. */
export function unsignedUrl(url: string | undefined): string | undefined {
  return url?.split('?')[0];
}

// Rejects when `signal` aborts, so an adapter that ignores the abort still cannot hold the queue.
function rejectOnAbort(signal: AbortSignal): Promise<never> {
  return new Promise((_resolve, reject) => {
    signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
  });
}

// The worker drains one track at a time, so a stalled transfer would wedge every pin behind it:
// each download gets its own deadline, and expiry rejects like any other failure. A failed
// download's partial file is removed so a later reconcile never adopts it as ready.
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
    // Only a completed download is counted, so removing the partial one subtracts nothing; a
    // partial that survives its delete leaves bytes the running total cannot account for.
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
