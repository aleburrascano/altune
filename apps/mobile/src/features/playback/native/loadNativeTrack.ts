import TrackPlayer, { type AddTrack } from 'react-native-track-player';

import { clamp } from '../clamp';
import { ensurePlayerSetup } from './initPlayer';
import { onNativeQueueTimeout, withNativeQueue, type Fence } from './nativeQueueLock';
import { activeNativeTrackId } from './nativeTrack';
import { nativeTrackBuilder } from './nativeTrackBuilder';
import { rebuildNativeTail, reorderUpcomingApplied } from './rebuildNativeTail';
import { forgetAllSwaps } from './nativeTrackSwap';
import { claimLoad, currentLoadToken, isStale, type LoadToken } from '../loadToken';
import { beginNativeLoad, endNativeLoad } from './nativeSyncGuard';
import { NATIVE_QUEUE_WINDOW, markPresignedFrom, extendPresignWindow } from '../presignWindow';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

export interface LoadNativeTrackOptions {
  autoplay?: boolean;
  startPositionMs?: number;
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

export function refreshUpcomingPresign(currentIndex: number): Promise<void> {
  return extendPresignWindow(currentIndex, reorderUpcomingApplied);
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

let reconcileInFlight = false;

async function reconcileUpcomingFromStore(): Promise<void> {
  if (reconcileInFlight) return;
  const state = useQueueStore.getState();
  const ordered = orderedQueueTracks(state);
  if (ordered.length === 0) return;
  reconcileInFlight = true;
  try {
    if ((await activeNativeTrackId()) === undefined) return;
    await rebuildNativeTail(ordered.slice(state.currentIndex + 1), currentLoadToken());
  } finally {
    reconcileInFlight = false;
  }
}

onNativeQueueTimeout(() => {
  reconcileUpcomingFromStore().catch(() => undefined);
});

async function resolveNative(track: PlaybackTrack): Promise<AddTrack> {
  await ensurePlayerSetup();
  const build = await nativeTrackBuilder([track]);
  return build(track);
}
