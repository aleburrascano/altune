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
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
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
    console.warn('[playback] presign failed', {
      trackIds: ids,
      error: redactedPlaybackFailure(err),
    });
    recordPresignOutcome(false);
    return { urls: new Map(), denied: classifyPlaybackFailure(err) === 'auth' };
  }
}

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
    if (isStale(token)) return;
    if (startPositionMs > 0) {
      await TrackPlayer.seekTo(startPositionMs / 1000);
      if (isStale(token)) return;
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

function tracksNativeHolds(
  tracks: readonly PlaybackTrack[],
  activeIndex: number,
): readonly PlaybackTrack[] {
  return tracks.slice(0, activeIndex + 1 + NATIVE_QUEUE_WINDOW);
}

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
      if (isStale(token)) return;
      if (idx > 0) {
        await TrackPlayer.skip(idx);
        if (isStale(token)) return;
      }
      if (startPositionMs > 0) {
        await TrackPlayer.seekTo(startPositionMs / 1000);
        if (isStale(token)) return;
      }
      if (autoplay) {
        await TrackPlayer.play();
      }
    } finally {
      endNativeLoad(generation);
    }
  });
}

function stillUpcoming(
  upcoming: readonly PlaybackTrack[],
  keyAtCall: string | undefined,
  keyNow: string | undefined,
): readonly PlaybackTrack[] {
  if (keyNow === undefined || keyNow === keyAtCall) return upcoming;
  const reached = upcoming.findIndex((t) => trackKey(t) === keyNow);
  return reached === -1 ? upcoming : upcoming.slice(reached + 1);
}

function livePositionOf(ordered: readonly PlaybackTrack[], cursor: number, key: string): number {
  const atCursor = ordered[cursor];
  if (atCursor !== undefined && trackKey(atCursor) === key) return cursor;
  return ordered.findIndex((t) => trackKey(t) === key);
}

function liveTailAfter(keyNow: string | undefined): readonly PlaybackTrack[] | null {
  if (keyNow === undefined) return null;
  const state = useQueueStore.getState();
  const ordered = orderedQueueTracks(state);
  const position = livePositionOf(ordered, state.currentIndex, keyNow);
  return position === -1 ? null : ordered.slice(position + 1);
}

interface RequestedTail {
  upcoming: readonly PlaybackTrack[];
  token: number;
}

let requestedTail: RequestedTail | null = null;
let rebuildInFlight: Promise<void> | null = null;

function takeRequestedTail(): RequestedTail | null {
  const tail = requestedTail;
  requestedTail = null;
  return tail;
}

export function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  requestedTail = { upcoming, token: currentLoadToken() };
  rebuildInFlight ??= Promise.resolve().then(rebuildRequestedTails);
  return rebuildInFlight;
}

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

async function rebuildNativeTail(upcoming: readonly PlaybackTrack[], token: number): Promise<void> {
  await ensurePlayerSetup();
  const [keyAtCall, headers] = await Promise.all([activeNativeTrackId(), headersFor(upcoming)]);
  const resolved = await resolveLibraryUrls(upcoming);
  await withNativeQueue(async () => {
    if (isStale(token)) return;
    const keyNow = await activeNativeTrackId();
    if (isStale(token)) return;
    const tail = liveTailAfter(keyNow) ?? stillUpcoming(upcoming, keyAtCall, keyNow);
    await TrackPlayer.removeUpcomingTracks();
    if (isStale(token)) return;
    const upcomingWindow = tail.slice(0, NATIVE_QUEUE_WINDOW);
    if (upcomingWindow.length === 0) return;
    await TrackPlayer.add(
      upcomingWindow.map((t) => toNativeTrack(t, { streamUrl: signedUrl(t, resolved), headers })),
    );
  });
}

export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return slidePresignWindow(currentIndex, reorderUpcomingNative);
}

function isInsideNativeWindow(queuePosition: number): boolean {
  return queuePosition <= NATIVE_QUEUE_WINDOW;
}

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
    if (isStale(token)) return;
    const held = await TrackPlayer.getQueue();
    const rebuiltFromStore =
      held.length >= useQueueStore.getState().playOrder.length &&
      held[position]?.id === trackKey(track);
    if (rebuiltFromStore) return;
    await TrackPlayer.add(native, position);
  });
}

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const headers = await headersFor([track]);
  const resolved = await resolveLibraryUrls([track]);
  return toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
}
