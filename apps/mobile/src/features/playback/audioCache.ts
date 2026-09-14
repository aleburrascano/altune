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

export function evictCached(trackId: string): void {
  try {
    for (const entry of cacheDir().list()) {
      if (entry instanceof File && baseName(entry.uri).startsWith(`${trackId}.`)) entry.delete();
    }
  } catch {}
}

export function evict(ordered: readonly PlaybackTrack[], currentIndex: number): void {
  const keep = new Set<string>();
  for (let i = currentIndex; i < ordered.length && i <= currentIndex + KEEP_WINDOW; i++) {
    const t = ordered[i];
    if (t && t.source.kind === 'library') keep.add(t.source.trackId);
  }
  try {
    for (const entry of cacheDir().list()) {
      if (!(entry instanceof File)) continue;
      const id = baseName(entry.uri).split('.')[0];
      if (id && !keep.has(id)) entry.delete();
    }
  } catch {}
}
