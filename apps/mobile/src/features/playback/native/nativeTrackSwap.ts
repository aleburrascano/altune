import TrackPlayer, { type AddTrack, type Track } from 'react-native-track-player';

import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import type { TrackId } from '@shared/api-client/ids';
import { currentLoadToken, isStale } from '../loadToken';
import { NativeQueueTimeoutError, withNativeQueue, type Fence } from './nativeQueueLock';
import { activeNativeTrackId, toNativeTrack } from './nativeTrack';
import { reportLoadFailure } from '../playbackErrorStore';
import { recordPresignOutcome } from '../playbackHealth';
import { redactedPlaybackFailure } from '../redactPlaybackError';

const LOAD_FAILED_MESSAGE = 'Could not load this track';

const swappedToLocal = new Set<string>();

export function wasSwappedToLocal(trackId: TrackId): boolean {
  return swappedToLocal.has(trackId);
}

export function forgetAllSwaps(): void {
  swappedToLocal.clear();
}

export function forgetSwap(trackId: TrackId): void {
  swappedToLocal.delete(trackId);
}

async function presignedUrlOrNull(trackId: TrackId): Promise<string | null> {
  try {
    const [resolved] = await fetchAudioUrls([trackId]);
    recordPresignOutcome(true);
    return resolved?.url ?? null;
  } catch (err) {
    console.warn('[playback] presign failed', {
      trackIds: [trackId],
      error: redactedPlaybackFailure(err),
    });
    recordPresignOutcome(false);
    return null;
  }
}

async function toStreamingNative(track: PlaybackTrack): Promise<AddTrack> {
  if (track.source.kind === 'preview') return toNativeTrack(track);
  const presignedUrl = await presignedUrlOrNull(track.source.trackId);
  if (presignedUrl) return toNativeTrack(track, { streamUrl: presignedUrl });
  return toNativeTrack(track, { headers: await audioRequestHeaders() });
}

async function loadAndPlay(native: AddTrack, fence: Fence): Promise<void> {
  fence();
  await TrackPlayer.load(native);
  fence();
  await TrackPlayer.play();
}

async function reloadActive(track: PlaybackTrack, native: AddTrack, fence: Fence): Promise<void> {
  try {
    await loadAndPlay(native, fence);
  } catch (err) {
    if (err instanceof NativeQueueTimeoutError) throw err;
    reportLoadFailure(track, err, LOAD_FAILED_MESSAGE);
  }
}

export async function repairActiveToStreaming(track: PlaybackTrack): Promise<void> {
  const token = currentLoadToken();
  const native = await toStreamingNative(track);
  await withNativeQueue(async (fence) => {
    if (isStale(token)) return;
    const activeKey = await activeNativeTrackId();
    if (activeKey !== undefined && activeKey !== trackKey(track)) return;
    if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
    await reloadActive(track, native, fence);
  });
}

interface UpcomingSlot {
  index: number;
  entry: Track;
}

async function upcomingSlotOf(key: string, fence: Fence): Promise<UpcomingSlot | null> {
  const queue = await TrackPlayer.getQueue().catch(() => []);
  fence();
  const activeIndex = await TrackPlayer.getActiveTrackIndex().catch(() => undefined);
  const after = activeIndex ?? -1;
  const index = queue.findIndex((t, i) => i > after && t.id === key);
  const entry = queue[index];
  return entry == null ? null : { index, entry };
}

export async function swapUpcomingToLocal(track: PlaybackTrack, uri: string): Promise<void> {
  await withNativeQueue(async (fence) => {
    const slot = await upcomingSlotOf(trackKey(track), fence);
    if (slot === null) return;

    fence();
    await TrackPlayer.remove(slot.index);
    await refillSlot({ slot, track, uri }, fence);
  });
}

interface Refill {
  slot: UpcomingSlot;
  track: PlaybackTrack;
  uri: string;
}

async function refillSlot(job: Refill, fence: Fence): Promise<void> {
  if (await refilledWithLocalFile(job, fence)) return;
  try {
    await refillStreaming(job, fence);
  } catch (err) {
    await restoreSlot(job.slot, fence);
    reportLoadFailure(job.track, err, LOAD_FAILED_MESSAGE);
  }
}

async function refillStreaming({ slot, track }: Refill, fence: Fence): Promise<void> {
  const native = await toStreamingNative(track);
  fence();
  await TrackPlayer.add(native, slot.index);
}

async function refilledWithLocalFile({ slot, track, uri }: Refill, fence: Fence): Promise<boolean> {
  fence();
  try {
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: uri }), slot.index);
  } catch {
    return false;
  }
  if (track.source.kind === 'library') swappedToLocal.add(track.source.trackId);
  return true;
}

function warnRestoreFailed(entry: Track, err: unknown): void {
  console.warn('[playback] swap slot restore failed', {
    trackId: entry.id,
    error: redactedPlaybackFailure(err),
  });
}

async function restoreSlot({ index, entry }: UpcomingSlot, fence: Fence): Promise<void> {
  fence();
  try {
    await TrackPlayer.add(entry, index);
  } catch (err) {
    warnRestoreFailed(entry, err);
  }
}
