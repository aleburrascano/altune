import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from 'react';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { shouldRestartOnPrevious } from '@shared/playback/constants';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey, type TrackKey } from '@shared/playback/trackKey';
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

interface PendingPresign {
  readonly key: TrackKey;
  readonly url: string;
  readonly issuedAt: number;
}

interface WebAudioPlayer {
  readonly audio: HTMLAudioElement;
  readonly update: (patch: Partial<WebPlayback>) => void;
  readonly now: () => number;
  loadSeq: number;
  awaitingSource: boolean;
  lastTrack: PlaybackTrack | null;
  sourceIssuedAt: number | null;
  recoveryAttempted: boolean;
  nextPresign: PendingPresign | null;
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

const PHASE_EVENTS = ['play', 'pause', 'waiting', 'seeked'];
const HAVE_FUTURE_DATA = 3;
const MEDIA_ERR_NETWORK = 2;
const MEDIA_ERR_DECODE = 3;
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;
const MEDIA_ERROR_KINDS: ReadonlyMap<number, PlaybackErrorKind> = new Map([
  [MEDIA_ERR_NETWORK, 'network'],
  [MEDIA_ERR_DECODE, 'decode'],
]);
const UNPLAYABLE_AUDIO = 'The audio could not be played';
const NO_AUDIO_URL: RedactedPlaybackFailure = {
  kind: 'not_found',
  message: 'No audio is available for this track',
};

const PRESIGN_TTL_MS = 60 * 60 * 1000;
const PRESIGN_REFRESH_MARGIN_MS = 60 * 1000;
const RECOVERABLE_ERROR_CODES: ReadonlySet<number> = new Set([
  MEDIA_ERR_NETWORK,
  MEDIA_ERR_SRC_NOT_SUPPORTED,
]);

function createAudioElement(): HTMLAudioElement {
  return new Audio();
}

function createWebAudioPlayer(
  audio: HTMLAudioElement,
  setPlayback: Dispatch<SetStateAction<WebPlayback>>,
  now: () => number,
): WebAudioPlayer {
  const update = (patch: Partial<WebPlayback>) => setPlayback((prev) => ({ ...prev, ...patch }));
  return {
    audio,
    update,
    now,
    loadSeq: 0,
    awaitingSource: false,
    lastTrack: null,
    sourceIssuedAt: null,
    recoveryAttempted: false,
    nextPresign: null,
  };
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

function isPresignStale(issuedAt: number, now: number): boolean {
  return now - issuedAt >= PRESIGN_TTL_MS - PRESIGN_REFRESH_MARGIN_MS;
}

function isSourceStale(player: WebAudioPlayer): boolean {
  if (player.sourceIssuedAt === null) return false;
  return isPresignStale(player.sourceIssuedAt, player.now());
}

function peekNextQueueTrack(): PlaybackTrack | null {
  const state = useQueueStore.getState();
  if (state.repeatMode === 'one') return null;
  const ordered = orderedQueueTracks(state);
  const next = ordered[state.currentIndex + 1];
  if (next) return next;
  return state.repeatMode === 'all' ? ordered[0] ?? null : null;
}

function isNextPresignFresh(player: WebAudioPlayer, key: TrackKey): boolean {
  const cached = player.nextPresign;
  return cached !== null && cached.key === key && !isPresignStale(cached.issuedAt, player.now());
}

async function prefetchNextTrack(player: WebAudioPlayer): Promise<void> {
  const next = peekNextQueueTrack();
  if (!next || next.source.kind === 'preview') return;
  const key = trackKey(next);
  if (isNextPresignFresh(player, key)) return;
  const issuedAt = player.now();
  const outcome = await resolveSource(next.source);
  if ('url' in outcome) player.nextPresign = { key, url: outcome.url, issuedAt };
}

function takeNextPresign(player: WebAudioPlayer, track: PlaybackTrack): PendingPresign | null {
  const cached = player.nextPresign;
  if (!cached || cached.key !== trackKey(track)) return null;
  if (isPresignStale(cached.issuedAt, player.now())) return null;
  player.nextPresign = null;
  return cached;
}

function mediaFailure(error: MediaError | null): RedactedPlaybackFailure {
  const kind = MEDIA_ERROR_KINDS.get(error?.code ?? 0) ?? 'unknown';
  const detail = error?.message ?? '';
  return { kind, message: redactPlaybackErrorMessage(detail === '' ? UNPLAYABLE_AUDIO : detail) };
}

function markAwaitingSource(player: WebAudioPlayer): void {
  player.awaitingSource = true;
  syncPhase(player);
}

async function represignAndResume(player: WebAudioPlayer, options: LoadOptions): Promise<void> {
  if (player.awaitingSource) return;
  const track = player.lastTrack;
  if (!track) return;
  const seq = player.loadSeq;
  markAwaitingSource(player);
  const outcome = await resolveSource(track.source);
  if (seq !== player.loadSeq) return;
  applySource(player, outcome, options);
}

function isRecoverableMediaError(error: MediaError | null): boolean {
  return RECOVERABLE_ERROR_CODES.has(error?.code ?? -1);
}

function recoverFromMediaError(player: WebAudioPlayer): Promise<void> {
  const startPositionMs = secondsToMs(player.audio.currentTime);
  return represignAndResume(player, { autoplay: true, startPositionMs });
}

function reportMediaError(player: WebAudioPlayer): void {
  if (player.awaitingSource) return;
  if (!player.recoveryAttempted && isRecoverableMediaError(player.audio.error) && isSourceStale(player)) {
    player.recoveryAttempted = true;
    void recoverFromMediaError(player);
    return;
  }
  player.update({ failure: mediaFailure(player.audio.error) });
}

function markRecovered(player: WebAudioPlayer): void {
  player.recoveryAttempted = false;
  syncPhase(player);
  void prefetchNextTrack(player);
}

function replayCurrentTrack(player: WebAudioPlayer): void {
  if (isSourceStale(player)) {
    void represignAndResume(player, { autoplay: true, startPositionMs: 0 });
    return;
  }
  player.audio.currentTime = 0;
  playAudio(player);
}

function advanceOnEnded(player: WebAudioPlayer): void {
  syncPhase(player);
  const { repeatMode } = useQueueStore.getState();
  if (repeatMode === 'one') {
    replayCurrentTrack(player);
    return;
  }
  void loadIfPresent(player, useQueueStore.getState().skipToNext());
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
  player.sourceIssuedAt = null;
  player.recoveryAttempted = false;
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

function applyResolvedSource(player: WebAudioPlayer, url: string, options: LoadOptions, issuedAt?: number): void {
  player.sourceIssuedAt = issuedAt ?? player.now();
  startSource(player, url, options);
}

function applySource(player: WebAudioPlayer, outcome: SourceOutcome, options: LoadOptions, issuedAt?: number): void {
  player.awaitingSource = false;
  if ('url' in outcome) applyResolvedSource(player, outcome.url, options, issuedAt);
  else player.update({ failure: outcome.failure });
}

async function loadTrack(
  player: WebAudioPlayer,
  track: PlaybackTrack,
  options: LoadOptions = {},
): Promise<void> {
  const seq = beginLoad(player, track);
  const cached = takeNextPresign(player, track);
  const outcome = cached ? { url: cached.url } : await resolveSource(track.source);
  if (seq === player.loadSeq) applySource(player, outcome, options, cached?.issuedAt);
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
  player.sourceIssuedAt = null;
  player.recoveryAttempted = false;
  player.nextPresign = null;
  releaseSource(player.audio);
  player.update(IDLE_PLAYBACK);
}

function endForSignOut(player: WebAudioPlayer): void {
  stopPlayer(player);
  useQueueStore.getState().clearQueue();
}

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
    void represignAndResume(player, { autoplay: !player.audio.paused, startPositionMs: positionMs });
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

function queueEditControls(
  player: WebAudioPlayer,
): Pick<PlaybackControls, 'reorderUpcoming' | 'appendToQueue' | 'insertNext' | 'removeQueueIndex'> {
  const onEdit = (): Promise<void> => onQueueEdited(player);
  return { reorderUpcoming: onEdit, appendToQueue: onEdit, insertNext: onEdit, removeQueueIndex: onEdit };
}

function createWebControls(player: WebAudioPlayer): PlaybackControls {
  return {
    ...loadControls(player),
    ...transportControls(player),
    ...skipControls(player),
    ...queueEditControls(player),
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
  return <PlaybackContext.Provider value={value}>{children}</PlaybackContext.Provider>;
}
