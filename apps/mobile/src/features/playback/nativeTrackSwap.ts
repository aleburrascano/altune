import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import { withNativeQueue } from './nativeQueueLock';
import { toNativeTrack } from './nativeTrack';
import { reportLoadFailure } from './playbackErrorStore';

const LOAD_FAILED_MESSAGE = 'Could not load this track';

const swappedToLocal = new Set<string>();

export function wasSwappedToLocal(trackId: string): boolean {
  return swappedToLocal.has(trackId);
}

export function forgetAllSwaps(): void {
  swappedToLocal.clear();
}

export function forgetSwap(trackId: string): void {
  swappedToLocal.delete(trackId);
}

async function presignedUrlOrNull(trackId: string): Promise<string | null> {
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    return resolved?.url ?? null;
  } catch (err) {
    // The track falls back to an authenticated stream URL; this trace is the only record that
    // the fallback fired, and the only one carrying the track id (the api-client log cannot —
    // fetchAudioUrls sends ids in the POST body, not the path it logs).
    console.warn('[playback] presign failed', { trackIds: [trackId], error: err });
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
  const native = await toStreamingNative(track);
  await withNativeQueue(async () => {
    const active = await TrackPlayer.getActiveTrack().catch(() => undefined);
    if (active != null && active.id !== trackKey(track)) return;
    if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
    try {
      await TrackPlayer.load(native);
      await TrackPlayer.play();
    } catch (err) {
      reportLoadFailure(track, err, LOAD_FAILED_MESSAGE);
    }
  });
}

async function upcomingSlotOf(key: string): Promise<number | null> {
  const queue = await TrackPlayer.getQueue().catch(() => []);
  const activeIndex = await TrackPlayer.getActiveTrackIndex().catch(() => undefined);
  const after = activeIndex ?? -1;
  const slot = queue.findIndex((t, i) => i > after && t.id === key);
  return slot < 0 ? null : slot;
}

/**
 * Rejects when the native remove fails, leaving the slot streaming: the caller owns the
 * trace and the health metric for a failed swap (`tracePrefetchFailure('swap', …)`), so
 * swallowing it here would hide the one prefetch failure mode that never reaches them.
 */
export async function swapUpcomingToLocal(track: PlaybackTrack, uri: string): Promise<void> {
  await withNativeQueue(async () => {
    const index = await upcomingSlotOf(trackKey(track));
    if (index === null) return;

    await TrackPlayer.remove(index);
    await refillSlot(index, track, uri);
  });
}

async function refillSlot(index: number, track: PlaybackTrack, uri: string): Promise<void> {
  try {
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: uri }), index);
    if (track.source.kind === 'library') swappedToLocal.add(track.source.trackId);
    return;
  } catch {}
  try {
    await TrackPlayer.add(await toStreamingNative(track), index);
  } catch (err) {
    reportLoadFailure(track, err, LOAD_FAILED_MESSAGE);
  }
}
