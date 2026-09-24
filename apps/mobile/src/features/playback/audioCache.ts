import { Directory, File, Paths } from 'expo-file-system';

import type { TrackId } from '@shared/api-client/ids';
import type { PlaybackTrack } from '@shared/playback/types';

const CACHE_SUBDIR = 'audio-prefetch';
const KEEP_WINDOW = 4;
const MB = 1024 * 1024;
// Total on-disk budget for prefetched audio, however large the individual source files are.
const MAX_CACHE_BYTES = 300 * MB;
// A single prefetch download is abandoned past this size, so the current and next tracks always
// fit within MAX_CACHE_BYTES together.
export const MAX_PREFETCH_FILE_BYTES = 150 * MB;

export function cacheDir(): Directory {
  const dir = new Directory(Paths.cache, CACHE_SUBDIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return dir;
}

function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

export function extFromUrl(url: string): string {
  const path = url.split('?')[0] ?? '';
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash ? path.slice(dot) : '.mp3';
}

export function buildCacheFileName(trackId: string, version: string, ext: string): string {
  return `${trackId}.${version}${ext}`;
}

export function buildPartialCacheFileName(trackId: string, version: string, ext: string): string {
  return `${buildCacheFileName(trackId, version, ext)}.part`;
}

function parseCacheFileName(name: string): { trackId: string; version: string; finished: boolean } {
  const [trackId = '', version = '', ...extension] = name.split('.');
  return { trackId, version, finished: extension.length === 1 };
}

export function findCached(trackId: TrackId, version: string): File | null {
  for (const entry of cacheDir().list()) {
    if (!(entry instanceof File)) continue;
    const cached = parseCacheFileName(baseName(entry.uri));
    if (cached.finished && cached.trackId === trackId && cached.version === version) return entry;
  }
  return null;
}

function cacheEntries(): (Directory | File)[] {
  try {
    return cacheDir().list();
  } catch {
    return [];
  }
}

function cachedFiles(): File[] {
  return cacheEntries().filter((entry): entry is File => entry instanceof File);
}

// One failed delete (e.g. a file still being written) must not abort the rest of the pass.
function deleteEach(entries: readonly (Directory | File)[]): void {
  for (const entry of entries) {
    try {
      entry.delete();
    } catch {}
  }
}

export function evictCached(trackId: TrackId): void {
  deleteEach(cachedFiles().filter((file) => trackIdOf(file) === trackId));
}

/**
 * Every entry, not only the ones a track id can be recovered from: an entry this module cannot
 * name is still the audio of whoever was signed in when it was written, so the retention rules
 * that keep the cache useful do not apply to it.
 */
export function evictAllCached(): void {
  deleteEach(cacheEntries());
}

function trackIdOf(file: File): string {
  return parseCacheFileName(baseName(file.uri)).trackId;
}

function sizeOf(file: File): number {
  try {
    return file.size ?? 0;
  } catch {
    return 0;
  }
}

// Library track ids in the retention window, nearest (the current track) first.
function windowIds(ordered: readonly PlaybackTrack[], currentIndex: number): string[] {
  const ids = new Set<string>();
  for (let i = currentIndex; i < ordered.length && i <= currentIndex + KEEP_WINDOW; i++) {
    const t = ordered[i];
    if (t && t.source.kind === 'library') ids.add(t.source.trackId);
  }
  return [...ids];
}

// Byte bound alongside the KEEP_WINDOW count: drop window files farthest from the current track
// until the cache fits. The current and next tracks are never dropped for size (either may be
// playing from its file); MAX_PREFETCH_FILE_BYTES keeps the two of them within the cap.
function enforceByteCap(
  kept: readonly File[],
  ordered: readonly PlaybackTrack[],
  currentIndex: number,
  maxBytes: number,
): void {
  // Sizes are read up front: a deleted file no longer reports one.
  const sizes = new Map(kept.map((file) => [file, sizeOf(file)]));
  let total = [...sizes.values()].reduce((sum, size) => sum + size, 0);
  const protectedIds = new Set<string>();
  for (const i of [currentIndex, currentIndex + 1]) {
    const t = ordered[i];
    if (t && t.source.kind === 'library') protectedIds.add(t.source.trackId);
  }
  const farthestFirst = windowIds(ordered, currentIndex).reverse();
  for (const id of farthestFirst) {
    if (total <= maxBytes) return;
    if (protectedIds.has(id)) continue;
    const files = kept.filter((file) => trackIdOf(file) === id);
    deleteEach(files);
    total -= files.reduce((sum, file) => sum + (sizes.get(file) ?? 0), 0);
  }
}

export function evict(
  ordered: readonly PlaybackTrack[],
  currentIndex: number,
  maxBytes: number = MAX_CACHE_BYTES,
): void {
  const keep = new Set(windowIds(ordered, currentIndex));
  const files = cachedFiles();
  deleteEach(
    files.filter((file) => {
      const id = trackIdOf(file);
      return id && !keep.has(id);
    }),
  );
  enforceByteCap(
    files.filter((file) => keep.has(trackIdOf(file))),
    ordered,
    currentIndex,
    maxBytes,
  );
}
