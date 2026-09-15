import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import { withNativeQueue } from './nativeQueueLock';
import { toNativeTrack } from './nativeTrack';
import { classifyPlaybackFailure, reportPlaybackError } from './playbackErrorStore';

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
  const native = await toStreamingNative(track);
  await withNativeQueue(async () => {
    const active = await TrackPlayer.getActiveTrack().catch(() => undefined);
    if (active != null && active.id !== trackKey(track)) return;
    if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
    try {
      await TrackPlayer.load(native);
      await TrackPlayer.play();
    } catch (err) {
      reportPlaybackError(
        trackKey(track),
        classifyPlaybackFailure(err),
        'Could not load this track',
      );
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

export async function swapUpcomingToLocal(track: PlaybackTrack, uri: string): Promise<void> {
  await withNativeQueue(async () => {
    const index = await upcomingSlotOf(trackKey(track));
    if (index === null) return;

    try {
      await TrackPlayer.remove(index);
    } catch {
      return;
    }
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
    reportPlaybackError(trackKey(track), classifyPlaybackFailure(err), 'Could not load this track');
  }
}
