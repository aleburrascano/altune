import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type JSX,
  type ReactNode,
  type SetStateAction,
} from 'react';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type {
  PlaybackContextValue,
  PlaybackControls,
  PlaybackErrorKind,
  PlaybackSource,
  PlaybackState,
  PlaybackTrack,
} from '@shared/playback/types';
import { onSignOut } from '@shared/session/signOutCleanup';

import { derivePlaybackState } from '../derivePlaybackState';
import {
  redactedPlaybackFailure,
  redactPlaybackErrorMessage,
  type RedactedPlaybackFailure,
} from '../redactPlaybackError';

type AudioPhase = 'loading' | 'playing' | 'paused' | 'ended';

interface WebPlayback {
  readonly track: PlaybackTrack | null;
  readonly phase: AudioPhase;
  readonly failure: RedactedPlaybackFailure | null;
  readonly positionMs: number;
  readonly durationMs: number;
}

interface WebAudioPlayer {
  readonly audio: HTMLAudioElement;
  readonly update: (patch: Partial<WebPlayback>) => void;
  loadSeq: number;
  awaitingSource: boolean;
  lastTrack: PlaybackTrack | null;
}

interface LoadOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

type SourceOutcome = { readonly url: string } | { readonly failure: RedactedPlaybackFailure };

const IDLE_PLAYBACK: WebPlayback = {
  track: null,
  phase: 'paused',
  failure: null,
  positionMs: 0,
  durationMs: 0,
};

const PHASE_EVENTS = ['play', 'playing', 'pause', 'waiting', 'seeked', 'ended'];
const HAVE_FUTURE_DATA = 3;
const MEDIA_ERR_NETWORK = 2;
const MEDIA_ERR_DECODE = 3;
const MEDIA_ERROR_KINDS: ReadonlyMap<number, PlaybackErrorKind> = new Map([
  [MEDIA_ERR_NETWORK, 'network'],
  [MEDIA_ERR_DECODE, 'decode'],
]);
const UNPLAYABLE_AUDIO = 'The audio could not be played';
const NO_AUDIO_URL: RedactedPlaybackFailure = {
  kind: 'not_found',
  message: 'No audio is available for this track',
};

function createAudioElement(): HTMLAudioElement {
  return new Audio();
}

function createWebAudioPlayer(
  audio: HTMLAudioElement,
  setPlayback: Dispatch<SetStateAction<WebPlayback>>,
): WebAudioPlayer {
  const update = (patch: Partial<WebPlayback>) => setPlayback((prev) => ({ ...prev, ...patch }));
  return { audio, update, loadSeq: 0, awaitingSource: false, lastTrack: null };
}

function secondsToMs(seconds: number): number {
  return Number.isFinite(seconds) ? Math.round(seconds * 1000) : 0;
}

function phaseOf(player: WebAudioPlayer): AudioPhase {
  const { audio } = player;
  if (player.awaitingSource) return 'loading';
  if (audio.ended) return 'ended';
  if (audio.paused) return 'paused';
  return audio.readyState < HAVE_FUTURE_DATA ? 'loading' : 'playing';
}

function syncPhase(player: WebAudioPlayer): void {
  player.update({ phase: phaseOf(player) });
}

function mediaFailure(error: MediaError | null): RedactedPlaybackFailure {
  const kind = MEDIA_ERROR_KINDS.get(error?.code ?? 0) ?? 'unknown';
  const detail = error?.message ?? '';
  return { kind, message: redactPlaybackErrorMessage(detail === '' ? UNPLAYABLE_AUDIO : detail) };
}

function reportMediaError(player: WebAudioPlayer): void {
  if (player.awaitingSource) return;
  player.update({ failure: mediaFailure(player.audio.error) });
}

function audioEventListeners(player: WebAudioPlayer): Record<string, () => void> {
  const sync = () => syncPhase(player);
  return {
    ...Object.fromEntries(PHASE_EVENTS.map((type) => [type, sync])),
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

function releaseSource(audio: HTMLAudioElement): void {
  audio.pause();
  audio.removeAttribute('src');
  audio.load();
}

async function resolveSource(source: PlaybackSource): Promise<SourceOutcome> {
  if (source.kind === 'preview') return { url: source.previewUrl };
  try {
    const urls = await fetchAudioUrls([source.trackId]);
    const match = urls.find((resolved) => resolved.trackId === source.trackId);
    return match ? { url: match.url } : { failure: NO_AUDIO_URL };
  } catch (err) {
    return { failure: redactedPlaybackFailure(err) };
  }
}

function playAudio(player: WebAudioPlayer): void {
  void player.audio.play().catch(() => syncPhase(player));
}

function beginLoad(player: WebAudioPlayer, track: PlaybackTrack): number {
  player.loadSeq += 1;
  player.awaitingSource = true;
  player.lastTrack = track;
  releaseSource(player.audio);
  player.update({ ...IDLE_PLAYBACK, track, phase: 'loading' });
  return player.loadSeq;
}

function startSource(player: WebAudioPlayer, url: string, options: LoadOptions): void {
  player.audio.src = url;
  player.audio.currentTime = (options.startPositionMs ?? 0) / 1000;
  if (options.autoplay === false) syncPhase(player);
  else playAudio(player);
}

function applySource(player: WebAudioPlayer, outcome: SourceOutcome, options: LoadOptions): void {
  player.awaitingSource = false;
  if ('url' in outcome) startSource(player, outcome.url, options);
  else player.update({ failure: outcome.failure });
}

async function loadTrack(
  player: WebAudioPlayer,
  track: PlaybackTrack,
  options: LoadOptions = {},
): Promise<void> {
  const seq = beginLoad(player, track);
  const outcome = await resolveSource(track.source);
  if (seq === player.loadSeq) applySource(player, outcome, options);
}

async function loadIfPresent(
  player: WebAudioPlayer,
  track: PlaybackTrack | null | undefined,
  options?: LoadOptions,
): Promise<void> {
  if (track) await loadTrack(player, track, options);
}

function stopPlayer(player: WebAudioPlayer): void {
  player.loadSeq += 1;
  player.awaitingSource = false;
  player.lastTrack = null;
  releaseSource(player.audio);
  player.update(IDLE_PLAYBACK);
}

function endForSignOut(player: WebAudioPlayer): void {
  stopPlayer(player);
  useQueueStore.getState().clearQueue();
}

function resumeAudio(player: WebAudioPlayer): void {
  if (player.audio.src !== '') playAudio(player);
}

function seekAudio(audio: HTMLAudioElement, positionMs: number): void {
  audio.currentTime = positionMs / 1000;
}

function setAudioRate(audio: HTMLAudioElement, rate: number): void {
  audio.playbackRate = rate;
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
    seekTo: (positionMs) => seekAudio(player.audio, positionMs),
    setRate: (rate) => setAudioRate(player.audio, rate),
    stop: () => stopPlayer(player),
  };
}

function skipControls(
  player: WebAudioPlayer,
): Pick<PlaybackControls, 'skipNext' | 'skipPrevious' | 'skipToQueueIndex'> {
  return {
    skipNext: () => loadIfPresent(player, useQueueStore.getState().skipToNext()),
    skipPrevious: () => loadIfPresent(player, useQueueStore.getState().skipToPrevious()),
    skipToQueueIndex: () => loadIfPresent(player, useQueueStore.getState().currentTrack()),
  };
}

const resolveWithoutEffect = (): Promise<void> => Promise.resolve();

const queueEditControls: Pick<
  PlaybackControls,
  'reorderUpcoming' | 'appendToQueue' | 'insertNext' | 'removeQueueIndex'
> = {
  reorderUpcoming: resolveWithoutEffect,
  appendToQueue: resolveWithoutEffect,
  insertNext: resolveWithoutEffect,
  removeQueueIndex: resolveWithoutEffect,
};

function createWebControls(player: WebAudioPlayer): PlaybackControls {
  return {
    ...loadControls(player),
    ...transportControls(player),
    ...skipControls(player),
    ...queueEditControls,
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

function useWebPlayback(createAudio: () => HTMLAudioElement): PlaybackContextValue {
  const [playback, setPlayback] = useState<WebPlayback>(IDLE_PLAYBACK);
  const [player] = useState(() => createWebAudioPlayer(createAudio(), setPlayback));
  const [controls] = useState(() => createWebControls(player));
  useEffect(() => attachPlayer(player), [player]);
  return useMemo(() => ({ ...stateOf(playback), ...controls }), [playback, controls]);
}

interface WebPlaybackProviderProps {
  children: ReactNode;
  createAudio?: () => HTMLAudioElement;
}

export function WebPlaybackProvider({
  children,
  createAudio = createAudioElement,
}: WebPlaybackProviderProps): JSX.Element {
  const value = useWebPlayback(createAudio);
  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
