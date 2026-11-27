import type { PlaybackControls, PlaybackTrack, QueueStore, RepeatMode } from '@shared/playback';

import type { PlayerPhase } from '@features/player/core/derivePlaybackState';

export interface NativePlaybackActions {
  controls: PlaybackControls;
  syncIsPlaying: (isPlaying: boolean) => void;
  rememberTrack: (track: PlaybackTrack) => void;
}

export interface PlaybackProgress {
  readonly position: number;
  readonly duration: number;
}

export interface TrackChanged {
  readonly kind: 'trackChanged';
  readonly lastIndex: number | undefined;
  readonly lastPositionSeconds: number | undefined;
  readonly lastTrackDurationSeconds: number | undefined;
}

export interface QueueEnded {
  readonly kind: 'queueEnded';
  readonly index: number;
  readonly positionSeconds: number | undefined;
}

export type PlaybackTransition = TrackChanged | QueueEnded;

export type SetDisplayedTrack = (track: PlaybackTrack | null) => void;

export type MarkPlaceholder = (generation: number) => void;

export type IsSkippable = (state: QueueStore) => boolean;

export interface NativePlayback {
  useProgress(updateIntervalMs: number): PlaybackProgress;
  usePhase(): PlayerPhase;
  useTransitions(onTransition: (transition: PlaybackTransition) => void): void;
  ensureSetup(): Promise<void>;
  applyRepeatMode(mode: RepeatMode): Promise<void>;
  createActions(setTrack: SetDisplayedTrack, initialIsPlaying: boolean): NativePlaybackActions;
  restoreSavedQueue(markPlaceholder: MarkPlaceholder): Promise<void>;
  createQueueSaver(isSkippable: IsSkippable): () => Promise<void>;
}
