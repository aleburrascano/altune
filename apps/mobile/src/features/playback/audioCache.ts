import { Directory, File, Paths } from 'expo-file-system';

import type { PlaybackTrack } from '@shared/playback/types';

const CACHE_SUBDIR = 'audio-prefetch';
const KEEP_WINDOW = 4;

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

export function findCached(trackId: string, version: string): File | null {
  for (const entry of cacheDir().list()) {
    if (entry instanceof File && baseName(entry.uri).startsWith(`${trackId}.${version}.`))
      return entry;
  }
  return null;
}

function cachedFiles(): File[] {
  try {
    return cacheDir()
      .list()
      .filter((entry): entry is File => entry instanceof File);
  } catch {
    return [];
  }
}

// One failed delete (e.g. a file still being written) must not abort the rest of the pass.
function deleteEach(files: readonly File[]): void {
  for (const file of files) {
    try {
      file.delete();
    } catch {}
  }
}

export function evictCached(trackId: string): void {
  deleteEach(cachedFiles().filter((file) => baseName(file.uri).startsWith(`${trackId}.`)));
}

export function evict(ordered: readonly PlaybackTrack[], currentIndex: number): void {
  const keep = new Set<string>();
  for (let i = currentIndex; i < ordered.length && i <= currentIndex + KEEP_WINDOW; i++) {
    const t = ordered[i];
    if (t && t.source.kind === 'library') keep.add(t.source.trackId);
  }
  deleteEach(
    cachedFiles().filter((file) => {
      const id = baseName(file.uri).split('.')[0];
      return id && !keep.has(id);
    }),
  );
}
