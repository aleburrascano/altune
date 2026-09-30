import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { ensurePlayerSetup } from './initPlayer';
import { withNativeQueue, type Fence } from './nativeQueueLock';
import { activeNativeTrackId } from './nativeTrack';
import { nativeTrackBuilder } from './nativeTrackBuilder';
import { currentLoadToken, isStale, type LoadToken } from '../loadToken';
import { NATIVE_QUEUE_WINDOW } from '../presignWindow';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

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

export function reorderUpcomingApplied(upcoming: readonly PlaybackTrack[]): Promise<boolean> {
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

export async function rebuildNativeTail(
  upcoming: readonly PlaybackTrack[],
  token: LoadToken,
): Promise<boolean> {
  await ensurePlayerSetup();
  const inputs = await tailInputs(upcoming);
  return withNativeQueue((fence) => applyTailLocked(inputs, token, fence));
}
