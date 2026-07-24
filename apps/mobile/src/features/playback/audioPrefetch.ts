import { Directory, File, Paths } from 'expo-file-system';
import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import { toNativeTrack } from './nativeTrack';

const CACHE_SUBDIR = 'audio-prefetch';
const KEEP_WINDOW = 4;

const inflight = new Set<string>();

const swappedToLocal = new Set<string>();

export function wasSwappedToLocal(trackId: string): boolean {
  return swappedToLocal.has(trackId);
}

export function forgetAllSwaps(): void {
  swappedToLocal.clear();
}

function cacheDir(): Directory {
  const dir = new Directory(Paths.cache, CACHE_SUBDIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return dir;
}

function baseName(uri: string): string {
  return uri.split('/').pop() ?? '';
}

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

async function presignedUrlOrNull(trackId: string): Promise<string | null> {
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    return resolved?.url ?? null;
  } catch {
    return null;
  }
}

async function toStreamingNative(track: PlaybackTrack): Promise<AddTrack> {
  if (track.source.kind === 'preview') return toNativeTrack(track);
  const presignedUrl = await presignedUrlOrNull(track.source.trackId);
  if (presignedUrl) return toNativeTrack(track, { streamUrl: presignedUrl });
  return toNativeTrack(track, { headers: await audioRequestHeaders() });
}

export async function repairActiveToStreaming(track: PlaybackTrack): Promise<void> {
  if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
  try {
    await TrackPlayer.load(await toStreamingNative(track));
    await TrackPlayer.play();
  } catch {}
}

export async function swapUpcomingToLocal(
  index: number,
  track: PlaybackTrack,
  uri: string,
): Promise<void> {
  const activeIndex = await TrackPlayer.getActiveTrackIndex().catch(() => undefined);
  if (activeIndex != null && index <= activeIndex) return;

  try {
    await TrackPlayer.remove(index);
  } catch {
    return;
  }

  try {
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: uri }), index);
    if (track.source.kind === 'library') swappedToLocal.add(track.source.trackId);
  } catch {
    await refillSlotWithStreamingEntry(index, track);
  }
}

async function refillSlotWithStreamingEntry(index: number, track: PlaybackTrack): Promise<void> {
  try {
    await TrackPlayer.add(await toStreamingNative(track), index);
  } catch {}
}

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
  } catch {}
}

export async function prefetchNext(activeIndex: number): Promise<void> {
  const s = useQueueStore.getState();
  const ordered = orderedQueueTracks(s);
  const next = ordered[activeIndex + 1];
  if (!next || next.source.kind !== 'library') return;
  const trackId = next.source.trackId;
  const start = Date.now();

  const existing = findCached(trackId);
  if (existing) {
    await swapUpcomingToLocal(activeIndex + 1, next, existing.uri);
    evict(ordered, s.currentIndex);
    console.log(
      `[audio-timing] prefetch-next track=${trackId} cache=hit swap_ms=${Date.now() - start}`,
    );
    return;
  }
  if (inflight.has(trackId)) return;
  inflight.add(trackId);
  try {
    const resolveStart = Date.now();
    const [resolved] = await fetchAudioUrls([trackId]);
    const resolveMs = Date.now() - resolveStart;
    if (!resolved) return;
    const dest = new File(cacheDir(), `${trackId}${extFromUrl(resolved.url)}`);
    const downloadStart = Date.now();
    const file = await File.downloadFileAsync(resolved.url, dest, { idempotent: true });
    const downloadMs = Date.now() - downloadStart;

    const s2 = useQueueStore.getState();
    const ordered2 = orderedQueueTracks(s2);
    const stillNext = ordered2[s2.currentIndex + 1];
    if (stillNext && stillNext.source.kind === 'library' && stillNext.source.trackId === trackId) {
      await swapUpcomingToLocal(s2.currentIndex + 1, stillNext, file.uri);
    }
    evict(ordered2, s2.currentIndex);
    console.log(
      `[audio-timing] prefetch-next track=${trackId} cache=miss resolve_ms=${resolveMs} download_ms=${downloadMs} total_ms=${Date.now() - start}`,
    );
  } catch {
  } finally {
    inflight.delete(trackId);
  }
}
