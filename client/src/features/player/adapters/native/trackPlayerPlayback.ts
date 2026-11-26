import TrackPlayer, {
  Event,
  RepeatMode,
  State,
  useProgress,
  usePlaybackState,
  useTrackPlayerEvents,
} from 'react-native-track-player';

import type { RepeatMode as QueueRepeatMode } from '@shared/playback';

import type { PlayerPhase } from '@features/player/domain/derivePlaybackState';
import {
  createNativePlaybackActions,
  ignoringNativeRejection,
} from '@features/player/adapters/native/createNativePlaybackActions';
import { ensurePlayerSetup } from '@features/player/adapters/native/initPlayer';
import { createQueueSaver } from '@features/player/adapters/native/queueSaver';
import { restoreSavedQueue } from '@features/player/adapters/native/restoreSavedQueue';
import type { NativePlayback, PlaybackTransition } from '@features/player/ports/nativePlayback';

const NATIVE_REPEAT: Record<QueueRepeatMode, RepeatMode> = {
  off: RepeatMode.Off,
  all: RepeatMode.Queue,
  one: RepeatMode.Track,
};

function phaseFromTrackPlayerState(tpState: State | undefined): PlayerPhase {
  if (tpState === State.Buffering || tpState === State.Loading) return 'loading';
  if (tpState === State.Ended) return 'ended';
  return tpState === State.Playing ? 'playing' : 'paused';
}

function usePhase(): PlayerPhase {
  return phaseFromTrackPlayerState(usePlaybackState().state);
}

function useTransitions(onTransition: (transition: PlaybackTransition) => void): void {
  useTrackPlayerEvents([Event.PlaybackActiveTrackChanged, Event.PlaybackQueueEnded], (event) => {
    if (event.type === Event.PlaybackActiveTrackChanged) {
      onTransition({
        kind: 'trackChanged',
        lastIndex: event.lastIndex,
        lastPositionSeconds: event.lastPosition,
        lastTrackDurationSeconds: event.lastTrack?.duration,
      });
      return;
    }
    onTransition({ kind: 'queueEnded', index: event.track, positionSeconds: event.position });
  });
}

export const trackPlayerPlayback: NativePlayback = {
  useProgress,
  usePhase,
  useTransitions,
  ensureSetup: () => ignoringNativeRejection(ensurePlayerSetup),
  applyRepeatMode: (mode) =>
    ignoringNativeRejection(async () => {
      await ensurePlayerSetup();
      await TrackPlayer.setRepeatMode(NATIVE_REPEAT[mode]);
    }),
  createActions: createNativePlaybackActions,
  restoreSavedQueue,
  createQueueSaver,
};
