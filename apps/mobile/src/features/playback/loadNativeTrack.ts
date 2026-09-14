import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { pinnedUri, repinIfStale } from '@shared/offline/pinnedStore';

import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { forgetAllSwaps } from './audioPrefetch';
import { ensurePlayerSetup } from './initPlayer';
import { withNativeQueue } from './nativeQueueLock';
import { toNativeTrack } from './nativeTrack';
import { claimLoad, currentSessionEpoch, isStale } from './loadToken';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import {
  MAX_PRESIGN,
  markPresignedFrom,
  refreshUpcomingPresign as slidePresignWindow,
} from './presignWindow';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

function headersFor(tracks: readonly PlaybackTrack[]): Promise<Record<string, string>> {
  const needsAuth = tracks.some((t) => t.source.kind === 'library');
  return needsAuth ? audioRequestHeaders() : Promise.resolve({});
}

async function resolveLibraryUrls(
  tracks: readonly PlaybackTrack[],
): Promise<Map<string, ResolvedAudioUrl>> {
  const ids: string[] = [];
  for (const t of tracks) {
    if (t.source.kind === 'library') ids.push(t.source.trackId);
    if (ids.length >= MAX_PRESIGN) break;
  }
  if (ids.length === 0) return new Map();
  try {
    const resolved = await fetchAudioUrls(ids);
    return new Map(resolved.map((r) => [r.trackId, r]));
  } catch {
    return new Map();
  }
}

function signedUrl(
  track: PlaybackTrack,
  resolved: Map<string, ResolvedAudioUrl>,
): string | undefined {
  if (track.source.kind !== 'library') return undefined;
  const match = resolved.get(track.source.trackId);
  repinIfStale(track.source.trackId, match?.version);
  return pinnedUri(track.source.trackId, match?.version) ?? match?.url;
}

export async function loadNativeTrack(
  track: PlaybackTrack,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await resetNative();
  if (isStale(token)) return;
  const headers = await headersFor([track]);
  const resolved = await resolveLibraryUrls([track]);
  if (isStale(token)) return;

  await withNativeQueue(async () => {
    if (isStale(token)) return;
    await TrackPlayer.add(toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers }));
    if (startPositionMs > 0) {
      await TrackPlayer.seekTo(startPositionMs / 1000);
    }
    if (autoplay) {
      await TrackPlayer.play();
    }
  });
}

function resetNative(): Promise<void> {
  return withNativeQueue(clearNativeQueue);
}

async function clearNativeQueue(): Promise<void> {
  await TrackPlayer.reset();
  forgetAllSwaps();
}

// A multi-track add is one logical operation, but a native failure can leave N of
// M tracks queued, out of step with queueStore. Already inside the queue lock, so
// reset directly (resetNative would deadlock) and surface the add error, not the
// rollback's. A stale load skips the rollback: an add that outlived the lock deadline
// may reject after a newer load has already rebuilt the queue.
async function addAllOrRollback(tracks: AddTrack[], token: number): Promise<void> {
  try {
    await TrackPlayer.add(tracks);
  } catch (err) {
    if (!isStale(token)) await clearNativeQueue().catch(() => undefined);
    throw err;
  }
}

export async function loadNativeQueue(
  tracks: readonly PlaybackTrack[],
  startIndex: number,
  options: LoadNativeTrackOptions = {},
): Promise<void> {
  const { autoplay = true, startPositionMs = 0 } = options;

  const token = claimLoad();
  await ensurePlayerSetup();
  if (isStale(token)) return;
  await resetNative();
  if (tracks.length === 0) return;

  const headers = await headersFor(tracks);
  const resolved = await resolveLibraryUrls(tracks.slice(startIndex));
  if (isStale(token)) return;

  const idx = Math.max(0, Math.min(startIndex, tracks.length - 1));
  markPresignedFrom(idx, tracks.length - idx);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const generation = beginNativeLoad(idx);
    try {
      await addAllOrRollback(
        tracks.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
        token,
      );
      if (idx > 0) await TrackPlayer.skip(idx);
      if (startPositionMs > 0) await TrackPlayer.seekTo(startPositionMs / 1000);
      if (autoplay) {
        await TrackPlayer.play();
      }
    } finally {
      endNativeLoad(generation);
    }
  });
}

function activeNativeKey(): Promise<string | undefined> {
  return TrackPlayer.getActiveTrack().then(
    (track) => (typeof track?.id === 'string' ? track.id : undefined),
    () => undefined,
  );
}

// Native auto-advances on its own clock, so the active track can change while the
// reorder resolves URLs outside the lock (holding the lock across that network call
// would stall skips and trip the lock deadline). removeUpcomingTracks trims relative
// to the track active *now*; if native moved onto one of `upcoming`, that track and
// everything before it are no longer upcoming (the store cursor syncs past them too),
// so re-adding them would duplicate the playing track.
function stillUpcoming(
  upcoming: readonly PlaybackTrack[],
  keyAtCall: string | undefined,
  keyNow: string | undefined,
): readonly PlaybackTrack[] {
  if (keyNow === undefined || keyNow === keyAtCall) return upcoming;
  const reached = upcoming.findIndex((t) => trackKey(t) === keyNow);
  return reached === -1 ? upcoming : upcoming.slice(reached + 1);
}

export async function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  const epoch = currentSessionEpoch();
  await ensurePlayerSetup();
  const [keyAtCall, headers] = await Promise.all([activeNativeKey(), headersFor(upcoming)]);
  const resolved = await resolveLibraryUrls(upcoming);
  await withNativeQueue(async () => {
    if (epoch !== currentSessionEpoch()) return;
    const tail = stillUpcoming(upcoming, keyAtCall, await activeNativeKey());
    await TrackPlayer.removeUpcomingTracks();
    if (tail.length === 0) return;
    await TrackPlayer.add(
      tail.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
  });
}

// The presign-window policy lives in ./presignWindow; this binds it to the native
// reorder so callers keep a single-argument entry point.
export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return slidePresignWindow(currentIndex, reorderUpcomingNative);
}

export async function appendNativeTrack(track: PlaybackTrack): Promise<void> {
  const epoch = currentSessionEpoch();
  const native = await resolveNative(track);
  await withNativeQueue(async () => {
    if (epoch === currentSessionEpoch()) await TrackPlayer.add(native);
  });
}

export async function insertNativeTrackNext(track: PlaybackTrack, position: number): Promise<void> {
  const epoch = currentSessionEpoch();
  const native = await resolveNative(track);
  await withNativeQueue(async () => {
    if (epoch === currentSessionEpoch()) await TrackPlayer.add(native, position);
  });
}

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const headers = await headersFor([track]);
  const resolved = await resolveLibraryUrls([track]);
  return toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
