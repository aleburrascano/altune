import TrackPlayer, { RepeatMode, State } from 'react-native-track-player';

import {
  addWidgetCommandListener,
  drainPendingCommands,
  reloadWidget,
  type WidgetCommandEvent,
} from '@modules/altune-widget';
import { withNativeQueue } from '@features/playback/native/nativeQueueLock';
import { reorderUpcomingNative } from '@features/playback/native/rebuildNativeTail';
import { reportingQueueFailure } from '@features/playback/native/queueFailureReport';
import { primeSignedInUser } from '@shared/auth/primeSignedInUser';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey, type TrackKey } from '@shared/playback/playbackTrack';
import type { RepeatMode as QueueRepeatMode } from '@shared/playback/queueOrder';
import { hasSignedInUser } from '@shared/session/signOutCleanup';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

type WidgetAction = () => Promise<void>;

export type WidgetCommandActions = { playPrevious: WidgetAction };

export function startWidgetCommands(actions: WidgetCommandActions): () => void {
  const widgetActions = widgetActionsFor(actions);
  const subscription = addWidgetCommandListener((event) => runWidgetCommand(widgetActions, event));
  drainPendingCommands();
  return () => subscription.remove();
}

function widgetActionsFor(actions: WidgetCommandActions): ReadonlyMap<string, WidgetAction> {
  return new Map([
    ['play-pause', togglePlayPause],
    ['next', skipToNext],
    ['previous', actions.playPrevious],
    ['shuffle', shuffleUpcoming],
    ['repeat', cycleRepeat],
  ]);
}

function runWidgetCommand(
  widgetActions: ReadonlyMap<string, WidgetAction>,
  { command }: WidgetCommandEvent,
): void {
  const action = widgetActions.get(command);
  if (action === undefined) return;
  void reportingQueueFailure(currentQueueTrackKey, `widget:${command}`, () =>
    runSignedInAction(action),
  );
}

async function runSignedInAction(action: WidgetAction): Promise<void> {
  if (!(await isSignedIn())) return;
  await action();
  reloadWidget();
}

async function isSignedIn(): Promise<boolean> {
  if (hasSignedInUser()) return true;
  await primeSignedInUser();
  return hasSignedInUser();
}

function currentQueueTrackKey(): TrackKey | null {
  const current = useQueueStore.getState().currentTrack();
  return current === null ? null : trackKey(current);
}

async function togglePlayPause(): Promise<void> {
  const { state } = await TrackPlayer.getPlaybackState();
  if (state === State.Playing) await TrackPlayer.pause();
  else await TrackPlayer.play();
}

function skipToNext(): Promise<void> {
  return withNativeQueue(() => TrackPlayer.skipToNext());
}

function shuffleUpcoming(): Promise<void> {
  return reorderUpcomingNative(useQueueStore.getState().toggleShuffle());
}

async function cycleRepeat(): Promise<void> {
  useQueueStore.getState().cycleRepeatMode();
  await TrackPlayer.setRepeatMode(NATIVE_REPEAT[useQueueStore.getState().repeatMode]);
}
