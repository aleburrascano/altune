import { shouldRestartOnPrevious } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackControls, PlaybackTrack } from '@shared/playback/types';

import { loadIfPresent, loadTrack, playAudio, represignAndResume, stopPlayer } from './webLoad';
import { isSourceStale, secondsToMs, type WebAudioPlayer } from './webPlayer';
import { prefetchNextTrack } from './webPresign';

function resumeAudio(player: WebAudioPlayer): void {
  if (player.audio.src === '') return;
  if (isSourceStale(player)) {
    const startPositionMs = secondsToMs(player.audio.currentTime);
    void represignAndResume(player, { autoplay: true, startPositionMs });
    return;
  }
  playAudio(player);
}

function seekAudio(player: WebAudioPlayer, positionMs: number): void {
  if (isSourceStale(player)) {
    void represignAndResume(player, {
      autoplay: !player.audio.paused,
      startPositionMs: positionMs,
    });
    return;
  }
  player.audio.currentTime = positionMs / 1000;
}

function setAudioRate(audio: HTMLAudioElement, rate: number): void {
  audio.playbackRate = rate;
  audio.defaultPlaybackRate = rate;
}

function playWithoutQueue(player: WebAudioPlayer, track: PlaybackTrack): Promise<void> {
  useQueueStore.getState().clearQueue();
  return loadTrack(player, track);
}

function loadControls(
  player: WebAudioPlayer,
): Pick<PlaybackControls, 'play' | 'startQueue' | 'retry'> {
  return {
    play: (track) => playWithoutQueue(player, track),
    startQueue: (ordered, startIndex, options) =>
      loadIfPresent(player, ordered[startIndex], options),
    retry: () => void loadIfPresent(player, player.lastTrack),
  };
}

type TransportControls = Pick<PlaybackControls, 'pause' | 'resume' | 'seekTo' | 'setRate' | 'stop'>;

function transportControls(player: WebAudioPlayer): TransportControls {
  return {
    pause: () => player.audio.pause(),
    resume: () => resumeAudio(player),
    seekTo: (positionMs) => seekAudio(player, positionMs),
    setRate: (rate) => setAudioRate(player.audio, rate),
    stop: () => stopPlayer(player),
  };
}

function skipToPreviousOrRestart(player: WebAudioPlayer): Promise<void> {
  if (shouldRestartOnPrevious(secondsToMs(player.audio.currentTime))) {
    seekAudio(player, 0);
    return Promise.resolve();
  }
  return loadIfPresent(player, useQueueStore.getState().skipToPrevious());
}

function skipControls(
  player: WebAudioPlayer,
): Pick<PlaybackControls, 'skipNext' | 'skipPrevious' | 'skipToQueueIndex'> {
  return {
    skipNext: () => loadIfPresent(player, useQueueStore.getState().skipToNext()),
    skipPrevious: () => skipToPreviousOrRestart(player),
    skipToQueueIndex: () => loadIfPresent(player, useQueueStore.getState().currentTrack()),
  };
}

function onQueueEdited(player: WebAudioPlayer): Promise<void> {
  void prefetchNextTrack(player);
  return Promise.resolve();
}

type QueueEditControls = Pick<
  PlaybackControls,
  'reorderUpcoming' | 'appendToQueue' | 'insertNext' | 'removeQueueIndex'
>;

function queueEditControls(player: WebAudioPlayer): QueueEditControls {
  const onEdit = (): Promise<void> => onQueueEdited(player);
  return {
    reorderUpcoming: onEdit,
    appendToQueue: onEdit,
    insertNext: onEdit,
    removeQueueIndex: onEdit,
  };
}

export function createWebControls(player: WebAudioPlayer): PlaybackControls {
  return {
    ...loadControls(player),
    ...transportControls(player),
    ...skipControls(player),
    ...queueEditControls(player),
  };
}
