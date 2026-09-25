import TrackPlayer, { type AddTrack, type Track } from 'react-native-track-player';

import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { audioRequestHeaders, fetchAudioUrls } from '@shared/api-client/audio';
import type { TrackId } from '@shared/api-client/ids';
import { currentLoadToken, isStale } from './loadToken';
import { withNativeQueue } from './nativeQueueLock';
import { activeNativeTrackId, toNativeTrack } from './nativeTrack';
import { reportLoadFailure } from './playbackErrorStore';
import { recordPresignOutcome } from './playbackHealth';
import { redactedPlaybackFailure } from './redactPlaybackError';

const LOAD_FAILED_MESSAGE = 'Could not load this track';

// Library ids whose native queue slot holds a cached file URI, which is how a playback error on
// them recovers. It mirrors the native queue, not the cache directory: routine cache eviction
// deletes files without rewriting the slots pointing at them, so an evicted track keeps its entry
// — that dangling slot is what `repairActiveToStreaming` exists to fix, and reclaiming the entry
// on eviction (#1734) would route the error to `recoverAudio`, which never reloads the player.
// An entry is dropped when its slot is rewritten, or with the whole native queue.
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
    // The track falls back to an authenticated stream URL; this trace is the only record that
    // the fallback fired, and the only one carrying the track id (the api-client log cannot —
    // fetchAudioUrls sends ids in the POST body, not the path it logs).
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

export async function repairActiveToStreaming(track: PlaybackTrack): Promise<void> {
  const token = currentLoadToken();
  const native = await toStreamingNative(track);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const activeKey = await activeNativeTrackId();
    if (activeKey !== undefined && activeKey !== trackKey(track)) return;
    if (track.source.kind === 'library') swappedToLocal.delete(track.source.trackId);
    try {
      await TrackPlayer.load(native);
      await TrackPlayer.play();
    } catch (err) {
      reportLoadFailure(track, err, LOAD_FAILED_MESSAGE);
    }
  });
}

interface UpcomingSlot {
  index: number;
  entry: Track;
}

async function upcomingSlotOf(key: string): Promise<UpcomingSlot | null> {
  const queue = await TrackPlayer.getQueue().catch(() => []);
  const activeIndex = await TrackPlayer.getActiveTrackIndex().catch(() => undefined);
  const after = activeIndex ?? -1;
  const index = queue.findIndex((t, i) => i > after && t.id === key);
  const entry = queue[index];
  return entry == null ? null : { index, entry };
}

/**
 * Rejects when the native remove fails, leaving the slot streaming: the caller owns the
 * trace and the health metric for a failed swap (`tracePrefetchFailure('swap', …)`), so
 * swallowing it here would hide the one prefetch failure mode that never reaches them.
 */
export async function swapUpcomingToLocal(track: PlaybackTrack, uri: string): Promise<void> {
  await withNativeQueue(async () => {
    const slot = await upcomingSlotOf(trackKey(track));
    if (slot === null) return;

    await TrackPlayer.remove(slot.index);
    await refillSlot(slot, track, uri);
  });
}

async function refillSlot(slot: UpcomingSlot, track: PlaybackTrack, uri: string): Promise<void> {
  if (await refilledWithLocalFile(slot, track, uri)) return;
  try {
    await TrackPlayer.add(await toStreamingNative(track), slot.index);
  } catch (err) {
    await restoreSlot(slot);
    reportLoadFailure(track, err, LOAD_FAILED_MESSAGE);
  }
}

async function refilledWithLocalFile(
  slot: UpcomingSlot,
  track: PlaybackTrack,
  uri: string,
): Promise<boolean> {
  try {
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: uri }), slot.index);
  } catch {
    return false;
  }
  if (track.source.kind === 'library') swappedToLocal.add(track.source.trackId);
  return true;
}

// Nothing took the slot the swap emptied, so the entry native already held goes back at the same
// index: the queue store still counts the track, and every index-based op after this one (skip,
// remove, reorder) addresses native by that store position. The original entry, not a rebuilt one,
// so whatever `swappedToLocal` already says about the slot stays true. A restore that itself fails
// leaves native one short of the store, and this trace is the only record of it.
async function restoreSlot({ index, entry }: UpcomingSlot): Promise<void> {
  try {
    await TrackPlayer.add(entry, index);
  } catch (err) {
    console.warn('[playback] swap slot restore failed', {
      trackId: entry.id,
      error: redactedPlaybackFailure(err),
    });
  }
}
