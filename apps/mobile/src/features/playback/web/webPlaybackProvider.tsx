import { useEffect, useMemo, useState, type ReactNode } from 'react';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue, PlaybackState } from '@shared/playback/types';
import { onSignOut } from '@shared/session/signOutCleanup';

import { derivePlaybackState } from '../derivePlaybackState';
import { useMediaSession } from '../hooks/useMediaSession';
import {
  advanceOnEnded,
  endForSignOut,
  markRecovered,
  reportMediaError,
  stopPlayer,
} from './webLoad';
import {
  createWebAudioPlayer,
  IDLE_PLAYBACK,
  secondsToMs,
  syncPhase,
  type WebAudioPlayer,
  type WebPlayback,
} from './webPlayer';
import { createWebControls } from './webControls';

const PHASE_EVENTS = ['play', 'pause', 'waiting', 'seeked'];

function createAudioElement(): HTMLAudioElement {
  return new Audio();
}

function audioEventListeners(player: WebAudioPlayer): Record<string, () => void> {
  const sync = () => syncPhase(player);
  return {
    ...Object.fromEntries(PHASE_EVENTS.map((type) => [type, sync])),
    playing: () => markRecovered(player),
    ended: () => advanceOnEnded(player),
    timeupdate: () => player.update({ positionMs: secondsToMs(player.audio.currentTime) }),
    durationchange: () => player.update({ durationMs: secondsToMs(player.audio.duration) }),
    error: () => reportMediaError(player),
  };
}

function listenToAudio(player: WebAudioPlayer): () => void {
  const listeners = Object.entries(audioEventListeners(player));
  for (const [type, listener] of listeners) player.audio.addEventListener(type, listener);
  return () => {
    for (const [type, listener] of listeners) player.audio.removeEventListener(type, listener);
  };
}

function stateOf({ phase, ...shown }: WebPlayback): PlaybackState {
  return derivePlaybackState({
    ...shown,
    isBuffering: phase === 'loading',
    isEnded: phase === 'ended',
    isPlaying: phase === 'playing',
  });
}

function attachPlayer(player: WebAudioPlayer): () => void {
  const stopListening = listenToAudio(player);
  const unregisterSignOut = onSignOut(() => endForSignOut(player));
  return () => {
    unregisterSignOut();
    stopListening();
    stopPlayer(player);
  };
}

function useWebPlayback(
  createAudio: () => HTMLAudioElement,
  now: () => number,
): PlaybackContextValue {
  const [playback, setPlayback] = useState<WebPlayback>(IDLE_PLAYBACK);
  const [player] = useState(() => createWebAudioPlayer(createAudio(), setPlayback, now));
  const [controls] = useState(() => createWebControls(player));
  useEffect(() => attachPlayer(player), [player]);
  return useMemo(() => ({ ...stateOf(playback), ...controls }), [playback, controls]);
}

interface WebPlaybackProviderProps {
  children: ReactNode;
  createAudio?: () => HTMLAudioElement;
  now?: () => number;
}

export function WebPlaybackProvider({
  children,
  createAudio = createAudioElement,
  now = Date.now,
}: WebPlaybackProviderProps): ReactNode {
  const value = useWebPlayback(createAudio, now);
  useMediaSession(value);
  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
