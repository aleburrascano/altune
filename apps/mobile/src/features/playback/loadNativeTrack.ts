import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { resolvePinnedUri } from '@shared/offline/pinnedStore';

import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { clamp } from './clamp';
import { classifyPlaybackFailure } from './classifyPlaybackError';
import { redactedPlaybackFailure } from './redactPlaybackError';
import { recordPresignOutcome } from './playbackHealth';
import { ensurePlayerSetup } from './initPlayer';
import { withNativeQueue } from './nativeQueueLock';
import { activeNativeTrackId, toNativeTrack } from './nativeTrack';
import { forgetAllSwaps } from './nativeTrackSwap';
import { claimLoad, currentLoadToken, isStale } from './loadToken';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import {
  MAX_PRESIGN,
  NATIVE_QUEUE_WINDOW,
  markPresignedFrom,
  refreshUpcomingPresign as slidePresignWindow,
} from './presignWindow';
import { useQueueStore } from '@shared/playback/queueStore';
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

// What one presign call learned. `denied` means the caller was refused outright, as opposed
// to a request that never got an answer.
interface ResolvedUrls {
  urls: Map<string, ResolvedAudioUrl>;
  denied: boolean;
}

async function resolveLibraryUrls(tracks: readonly PlaybackTrack[]): Promise<ResolvedUrls> {
  const ids: string[] = [];
  for (const t of tracks) {
    if (t.source.kind === 'library') ids.push(t.source.trackId);
    if (ids.length >= MAX_PRESIGN) break;
  }
  if (ids.length === 0) return { urls: new Map(), denied: false };
  try {
    const resolved = await fetchAudioUrls(ids);
    recordPresignOutcome(true);
    return { urls: new Map(resolved.map((r) => [r.trackId, r])), denied: false };
  } catch (err) {
    // The load falls back to streaming each track; the trace records that the fallback fired.
    // Today's presign rejections carry no URL, but nothing here would notice if one started to,
    // so the rejection is redacted rather than logged whole.
    console.warn('[playback] presign failed', {
      trackIds: ids,
      error: redactedPlaybackFailure(err),
    });
    recordPresignOutcome(false);
    return { urls: new Map(), denied: classifyPlaybackFailure(err) === 'auth' };
  }
}

// A pinned (downloaded) file plays without asking the server, so it is served only when
// presign did not deny the caller: offline, pinned tracks still play; once access is
// refused, the track streams and the stream endpoint enforces authorization itself.
function signedUrl(track: PlaybackTrack, resolved: ResolvedUrls): string | undefined {
  if (track.source.kind !== 'library' || resolved.denied) return undefined;
  const match = resolved.urls.get(track.source.trackId);
  return resolvePinnedUri(track.source.trackId, match?.version) ?? match?.url;
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

// The native player is never handed the whole queue: it holds it from the start through
// NATIVE_QUEUE_WINDOW tracks past the active one, and each presign refresh slides that
// edge forward. Keeping the already-played head is what lets a native index stay the
// store's own queue position, which native back-skip and every index-based native call
// (skip, remove, insert) read as such.
function tracksNativeHolds(
  tracks: readonly PlaybackTrack[],
  activeIndex: number,
): readonly PlaybackTrack[] {
  return tracks.slice(0, activeIndex + 1 + NATIVE_QUEUE_WINDOW);
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

  const idx = clamp(startIndex, 0, tracks.length - 1);
  markPresignedFrom(idx, tracks.length - idx);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const generation = beginNativeLoad(idx);
    try {
      await addAllOrRollback(
        tracksNativeHolds(tracks, idx).map((t) =>
          toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers }),
        ),
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

// One reorder the native tail has not been given yet.
interface RequestedTail {
  upcoming: readonly PlaybackTrack[];
  // Claimed when the caller asked, not when the rebuild runs: a load claimed mid-burst
  // must still void the rebuild at the lock instead of pushing this tail onto its queue.
  token: number;
}

// The newest requested order, and the rebuild that will apply it.
let requestedTail: RequestedTail | null = null;
let rebuildInFlight: Promise<void> | null = null;

function takeRequestedTail(): RequestedTail | null {
  const tail = requestedTail;
  requestedTail = null;
  return tail;
}

/**
 * Hands the store's upcoming tracks to the native player, coalescing a burst onto the
 * last of them. Resolves once native holds an order at least as new as this caller's.
 *
 * Every caller passes the whole upcoming list from the current position, so the newest
 * request already describes the queue the older ones were aiming at: applying only it
 * lands the same final order for one presign round trip and one rebuild instead of one
 * of each per tap. Requests made in one tick collapse onto the first rebuild; requests
 * made while a rebuild runs collapse onto a single trailing one, which is where a long
 * restored queue spends a burst of "move up" taps.
 */
export function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  requestedTail = { upcoming, token: currentLoadToken() };
  rebuildInFlight ??= Promise.resolve().then(rebuildRequestedTails);
  return rebuildInFlight;
}

// A rejection abandons whatever was requested during the failed rebuild: the caller
// reports the failure as a resync prompt, and a rebuild still running behind that prompt
// would contradict it. Recovery is the retry that reloads the queue from the store.
async function rebuildRequestedTails(): Promise<void> {
  try {
    let tail = takeRequestedTail();
    while (tail !== null) {
      await rebuildNativeTail(tail.upcoming, tail.token);
      tail = takeRequestedTail();
    }
  } finally {
    requestedTail = null;
    rebuildInFlight = null;
  }
}

// Rebuilds the native tail in the store's ordering, windowed: only the first
// NATIVE_QUEUE_WINDOW upcoming tracks are pushed, so a 2000-track queue costs the same
// bridge payload here as a 100-track one and the rest arrive on a later slide.
async function rebuildNativeTail(upcoming: readonly PlaybackTrack[], token: number): Promise<void> {
  await ensurePlayerSetup();
  const [keyAtCall, headers] = await Promise.all([activeNativeTrackId(), headersFor(upcoming)]);
  const resolved = await resolveLibraryUrls(upcoming);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const tail = stillUpcoming(upcoming, keyAtCall, await activeNativeTrackId());
    await TrackPlayer.removeUpcomingTracks();
    const upcomingWindow = tail.slice(0, NATIVE_QUEUE_WINDOW);
    if (upcomingWindow.length === 0) return;
    await TrackPlayer.add(
      upcomingWindow.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
  });
}

// The presign-window policy lives in ./presignWindow; this binds it to the native
// reorder so callers keep a single-argument entry point.
export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return slidePresignWindow(currentIndex, reorderUpcomingNative);
}

// A native add lands at the end of what native holds, which is the end of the *queue*
// only while the window still reaches it. Past that edge the append would play right
// after the window instead of last, so it is left to the next slide, which rebuilds the
// upcoming tracks from the store in order. Whatever the active track, native holds at
// least the first NATIVE_QUEUE_WINDOW positions, so that bound needs no native round trip.
function isInsideNativeWindow(queuePosition: number): boolean {
  return queuePosition <= NATIVE_QUEUE_WINDOW;
}

/** Call after the store append: the track is read as sitting at the end of the queue. */
export async function appendNativeTrack(track: PlaybackTrack): Promise<void> {
  const token = currentLoadToken();
  if (!isInsideNativeWindow(useQueueStore.getState().playOrder.length - 1)) return;
  const native = await resolveNative(track);
  await withNativeQueue(async () => {
    if (!isStale(token)) await TrackPlayer.add(native);
  });
}

export async function insertNativeTrackNext(track: PlaybackTrack, position: number): Promise<void> {
  const token = currentLoadToken();
  const native = await resolveNative(track);
  await withNativeQueue(async () => {
    if (!isStale(token)) await TrackPlayer.add(native, position);
  });
}

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const headers = await headersFor([track]);
  const resolved = await resolveLibraryUrls([track]);
  return toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
