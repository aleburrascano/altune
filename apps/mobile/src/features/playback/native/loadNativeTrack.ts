import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { resolvePinnedUri } from '@shared/offline/pinnedStore';

import {
  audioRequestHeaders,
  fetchAudioUrls,
  type ResolvedAudioUrl,
} from '@shared/api-client/audio';
import { clamp } from '../clamp';
import { classifyPlaybackFailure } from '../classifyPlaybackError';
import { warnPlayback } from '../redactPlaybackError';
import { recordPresignOutcome } from '../playbackHealth';
import { ensurePlayerSetup } from './initPlayer';
import { onNativeQueueTimeout, withNativeQueue, type Fence } from './nativeQueueLock';
import { activeNativeTrackId, toNativeTrack } from './nativeTrack';
import { forgetAllSwaps } from './nativeTrackSwap';
import { claimLoad, currentLoadToken, isStale, type LoadToken } from '../loadToken';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import {
  MAX_PRESIGN,
  NATIVE_QUEUE_WINDOW,
  markPresignedFrom,
  refreshUpcomingPresign as slidePresignWindow,
} from '../presignWindow';
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
    warnPlayback('presign failed', { trackIds: ids }, err);
    recordPresignOutcome(false);
    return { urls: new Map(), denied: classifyPlaybackFailure(err) === 'auth' };
  }
}

function signedUrl(track: PlaybackTrack, resolved: ResolvedUrls): string | undefined {
  if (track.source.kind !== 'library' || resolved.denied) return undefined;
  const match = resolved.urls.get(track.source.trackId);
  return resolvePinnedUri(track.source.trackId, match?.version) ?? match?.url;
}

async function nativeTrackBuilder(
  signFor: readonly PlaybackTrack[],
  headersForTracks: readonly PlaybackTrack[] = signFor,
): Promise<(track: PlaybackTrack) => AddTrack> {
  const headers = await headersFor(headersForTracks);
  const resolved = await resolveLibraryUrls(signFor);
  return (track) => toNativeTrack(track, { streamUrl: signedUrl(track, resolved), headers });
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
  const build = await nativeTrackBuilder([track]);
  if (isStale(token)) return;

  await withNativeQueue(async () => {
    if (isStale(token)) return;
    await TrackPlayer.add(build(track));
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

export async function clearNativeQueue(): Promise<void> {
  await TrackPlayer.reset();
  forgetAllSwaps();
}

function tracksNativeHolds(
  tracks: readonly PlaybackTrack[],
  activeIndex: number,
): readonly PlaybackTrack[] {
  return tracks.slice(0, activeIndex + 1 + NATIVE_QUEUE_WINDOW);
}

async function rollbackUnlessStale(token: LoadToken, fence: Fence): Promise<void> {
  if (isStale(token)) return;
  fence();
  await clearNativeQueue().catch(() => undefined);
}

async function addAllOrRollback(tracks: AddTrack[], token: LoadToken, fence: Fence): Promise<void> {
  try {
    await TrackPlayer.add(tracks);
  } catch (err) {
    await rollbackUnlessStale(token, fence);
    throw err;
  }
}

interface StartPlan {
  idx: number;
  startPositionMs: number;
  autoplay: boolean;
  token: LoadToken;
}

async function skipTo(idx: number, token: LoadToken, fence: Fence): Promise<boolean> {
  if (idx <= 0) return true;
  fence();
  await TrackPlayer.skip(idx);
  return !isStale(token);
}

async function seekToStart(ms: number, token: LoadToken, fence: Fence): Promise<boolean> {
  if (ms <= 0) return true;
  fence();
  await TrackPlayer.seekTo(ms / 1000);
  return !isStale(token);
}

async function startAt(plan: StartPlan, fence: Fence): Promise<void> {
  if (!(await skipTo(plan.idx, plan.token, fence))) return;
  if (!(await seekToStart(plan.startPositionMs, plan.token, fence))) return;
  if (!plan.autoplay) return;
  fence();
  await TrackPlayer.play();
}

async function addAndStart(native: AddTrack[], plan: StartPlan, fence: Fence): Promise<void> {
  await addAllOrRollback(native, plan.token, fence);
  if (isStale(plan.token)) return;
  await startAt(plan, fence);
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

  const idx = clamp(startIndex, 0, tracks.length - 1);
  const build = await nativeTrackBuilder(tracks.slice(idx), tracks);
  if (isStale(token)) return;

  markPresignedFrom(idx, tracks.length - idx);
  const plan: StartPlan = { idx, startPositionMs, autoplay, token };
  await withNativeQueue(async (fence) => {
    if (isStale(token)) return;
    const generation = beginNativeLoad(idx);
    try {
      await addAndStart(tracksNativeHolds(tracks, idx).map(build), plan, fence);
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
  token: LoadToken;
}

let requestedTail: RequestedTail | null = null;
function takeRequestedTail(): RequestedTail | null {
  const tail = requestedTail;
  requestedTail = null;
  return tail;
}

let rebuildInFlight: Promise<boolean> | null = null;

function reorderUpcomingApplied(upcoming: readonly PlaybackTrack[]): Promise<boolean> {
  requestedTail = { upcoming, token: currentLoadToken() };
  rebuildInFlight ??= Promise.resolve().then(rebuildRequestedTails);
  return rebuildInFlight;
}

export async function reorderUpcomingNative(upcoming: readonly PlaybackTrack[]): Promise<void> {
  await reorderUpcomingApplied(upcoming);
}

async function drainRequestedTails(): Promise<boolean> {
  let applied = false;
  for (let tail = takeRequestedTail(); tail !== null; tail = takeRequestedTail()) {
    applied = await rebuildNativeTail(tail.upcoming, tail.token);
  }
  return applied;
}

async function rebuildRequestedTails(): Promise<boolean> {
  try {
    return await drainRequestedTails();
  } finally {
    requestedTail = null;
    rebuildInFlight = null;
  }
}

async function upcomingHeldNatively(fence: Fence): Promise<AddTrack[]> {
  fence();
  const [held, activeIndex] = await Promise.all([
    TrackPlayer.getQueue(),
    TrackPlayer.getActiveTrackIndex(),
  ]);
  return activeIndex === undefined ? [] : (held ?? []).slice(activeIndex + 1);
}

async function restoreUpcoming(previous: AddTrack[], fence: Fence): Promise<void> {
  fence();
  if (previous.length > 0) await TrackPlayer.add(previous).catch(() => undefined);
}

async function addWindow(window: AddTrack[], previous: AddTrack[], fence: Fence): Promise<boolean> {
  try {
    fence();
    if (window.length > 0) await TrackPlayer.add(window);
    return true;
  } catch (err) {
    await restoreUpcoming(previous, fence);
    throw err;
  }
}

async function detachUpcoming(fence: Fence): Promise<AddTrack[]> {
  const previous = await upcomingHeldNatively(fence);
  fence();
  await TrackPlayer.removeUpcomingTracks();
  return previous;
}

async function replaceUpcomingOrRestore(
  window: AddTrack[],
  token: LoadToken,
  fence: Fence,
): Promise<boolean> {
  const previous = await detachUpcoming(fence);
  if (!isStale(token)) return addWindow(window, previous, fence);
  await restoreUpcoming(previous, fence);
  return false;
}

interface TailInputs {
  upcoming: readonly PlaybackTrack[];
  keyAtCall: string | undefined;
  build: (track: PlaybackTrack) => AddTrack;
}

async function tailInputs(upcoming: readonly PlaybackTrack[]): Promise<TailInputs> {
  const [keyAtCall, build] = await Promise.all([
    activeNativeTrackId(),
    nativeTrackBuilder(upcoming),
  ]);
  return { upcoming, keyAtCall, build };
}

function windowFor(inputs: TailInputs, keyNow: string | undefined): AddTrack[] {
  const tail = liveTailAfter(keyNow) ?? stillUpcoming(inputs.upcoming, inputs.keyAtCall, keyNow);
  return tail.slice(0, NATIVE_QUEUE_WINDOW).map(inputs.build);
}

async function applyTailLocked(
  inputs: TailInputs,
  token: LoadToken,
  fence: Fence,
): Promise<boolean> {
  if (isStale(token)) return false;
  const keyNow = await activeNativeTrackId();
  if (isStale(token)) return false;
  return replaceUpcomingOrRestore(windowFor(inputs, keyNow), token, fence);
}

async function rebuildNativeTail(
  upcoming: readonly PlaybackTrack[],
  token: LoadToken,
): Promise<boolean> {
  await ensurePlayerSetup();
  const inputs = await tailInputs(upcoming);
  return withNativeQueue((fence) => applyTailLocked(inputs, token, fence));
}

export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return slidePresignWindow(currentIndex, reorderUpcomingApplied);
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
  await withNativeQueue(async (fence) => {
    if (isStale(token)) return;
    const held = await TrackPlayer.getQueue();
    const rebuiltFromStore =
      held.length >= useQueueStore.getState().playOrder.length &&
      held[position]?.id === trackKey(track);
    if (rebuiltFromStore) return;
    fence();
    await TrackPlayer.add(native, position);
  });
}

function reconcileUpcomingFromStore(): void {
  const state = useQueueStore.getState();
  const ordered = orderedQueueTracks(state);
  if (ordered.length === 0) return;
  rebuildNativeTail(ordered.slice(state.currentIndex + 1), currentLoadToken()).catch(
    () => undefined,
  );
}

onNativeQueueTimeout(reconcileUpcomingFromStore);

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const build = await nativeTrackBuilder([track]);
  return build(track);
}
