import { Directory, File, Paths } from 'expo-file-system';
import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import { toNativeTrack } from './nativeTrack';
import { reportPlaybackError } from './playbackErrorStore';

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
  const active = await TrackPlayer.getActiveTrack().catch(() => undefined);
  if (active != null && active.id !== trackKey(track)) return;
  if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
  try {
    await TrackPlayer.load(await toStreamingNative(track));
    await TrackPlayer.play();
  } catch {}
}

async function upcomingSlotOf(key: string): Promise<number | null> {
  const queue = await TrackPlayer.getQueue().catch(() => []);
  const activeIndex = await TrackPlayer.getActiveTrackIndex().catch(() => undefined);
  const after = activeIndex ?? -1;
  const slot = queue.findIndex((t, i) => i > after && t.id === key);
  return slot < 0 ? null : slot;
}

export async function swapUpcomingToLocal(track: PlaybackTrack, uri: string): Promise<void> {
  const key = trackKey(track);
  const index = await upcomingSlotOf(key);
  if (index === null) return;

  try {
    await TrackPlayer.remove(index);
  } catch {
    return;
  }
  await refillSlot(index, track, uri);
}

async function refillSlot(index: number, track: PlaybackTrack, uri: string): Promise<void> {
  try {
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: uri }), index);
    if (track.source.kind === 'library') swappedToLocal.add(track.source.trackId);
    return;
  } catch {}
  try {
    await TrackPlayer.add(await toStreamingNative(track), index);
  } catch {
    reportPlaybackError(trackKey(track), 'Could not load this track');
  }
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
    await swapUpcomingToLocal(next, existing.uri);
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
      await swapUpcomingToLocal(stillNext, file.uri);
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
