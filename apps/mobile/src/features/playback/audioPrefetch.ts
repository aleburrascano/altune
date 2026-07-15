import { Directory, File, Paths } from 'expo-file-system';
import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { fetchAudioUrls } from './api/audio';

// AIDEV-NOTE: Download-ahead cache. RNTP's iOS backend does not pre-buffer the
// next queue item, so every auto-advance stalls ~1s buffering the next remote
// stream from scratch. This module downloads the *next* track to a local file
// while the current one plays and swaps its (still-upcoming) native queue entry
// to the local path — so when the boundary hits, the player loads from disk with
// zero buffering. Everything here is best-effort: any failure leaves the
// streaming (presigned/proxy) URL in place, so playback degrades, never breaks.

const CACHE_SUBDIR = 'audio-prefetch';
// Keep the current track plus a short forward window on disk; evict the rest so
// the cache stays bounded (a handful of songs, not the whole library).
const KEEP_WINDOW = 4;

// Dedupe concurrent prefetches of the same track.
const inflight = new Set<string>();

function cacheDir(): Directory {
  const dir = new Directory(Paths.cache, CACHE_SUBDIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return dir;
}

function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

// Extension of the object behind a (presigned) URL, so the local file keeps the
// container hint the native decoder uses. Falls back to .mp3.
function extFromUrl(url: string): string {
  const path = url.split('?')[0] ?? '';
  const slash = path.lastIndexOf('/');
  const dot = path.lastIndexOf('.');
  return dot > slash ? path.slice(dot) : '.mp3';
}

function findCached(trackId: string): File | null {
  for (const entry of cacheDir().list()) {
    if (entry instanceof File && baseName(entry.uri).startsWith(`${trackId}.`)) return entry;
  }
  return null;
}

function toLocalNative(track: PlaybackTrack, uri: string): AddTrack {
  return { url: uri, title: track.title, artist: track.artist, artwork: track.artworkUrl ?? '' };
}

async function swapUpcomingToLocal(index: number, track: PlaybackTrack, uri: string): Promise<void> {
  try {
    await TrackPlayer.remove(index);
    await TrackPlayer.add(toLocalNative(track, uri), index);
  } catch {
    // native queue shifted (advanced / rebuilt) — leave the streaming URL as is
  }
}

// Delete cached files for tracks outside the current..+KEEP_WINDOW window.
function evict(ordered: readonly PlaybackTrack[], currentIndex: number): void {
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
  } catch {
    // ignore — eviction is best-effort
  }
}

// Prefetch the track after `activeIndex` and, once local, swap its still-upcoming
// native queue entry to the local file so auto-advance plays from disk instantly.
export async function prefetchNext(activeIndex: number): Promise<void> {
  const s = useQueueStore.getState();
  const ordered = orderedQueueTracks(s);
  const next = ordered[activeIndex + 1];
  if (!next || next.source.kind !== 'library') return;
  const trackId = next.source.trackId;

  const existing = findCached(trackId);
  if (existing) {
    await swapUpcomingToLocal(activeIndex + 1, next, existing.uri);
    evict(ordered, s.currentIndex);
    return;
  }
  if (inflight.has(trackId)) return;
  inflight.add(trackId);
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    if (!resolved) return;
    const dest = new File(cacheDir(), `${trackId}${extFromUrl(resolved.url)}`);
    const file = await File.downloadFileAsync(resolved.url, dest, { idempotent: true });

    // The queue may have advanced or been rebuilt during the download — only
    // swap if this is still the immediate next track.
    const s2 = useQueueStore.getState();
    const ordered2 = orderedQueueTracks(s2);
    const stillNext = ordered2[s2.currentIndex + 1];
    if (stillNext && stillNext.source.kind === 'library' && stillNext.source.trackId === trackId) {
      await swapUpcomingToLocal(s2.currentIndex + 1, stillNext, file.uri);
    }
    evict(ordered2, s2.currentIndex);
  } catch {
    // best-effort — the streaming URL remains playable
  } finally {
    inflight.delete(trackId);
  }
}

// Clear every prefetched file (e.g. on sign-out or a full queue teardown).
export function clearPrefetchCache(): void {
  try {
    const dir = cacheDir();
    if (dir.exists) dir.delete();
  } catch {
    // ignore
  }
}
