import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackErrorKind, PlaybackTrack } from '@shared/playback/types';

import { recordPlaybackFailure } from '../playbackHealth';
import {
  redactPlaybackErrorMessage,
  warnPlayback,
  type RedactedPlaybackFailure,
} from '../redactPlaybackError';
import {
  IDLE_PLAYBACK,
  isSourceStale,
  secondsToMs,
  syncPhase,
  type LoadOptions,
  type SourceOutcome,
  type WebAudioPlayer,
} from './webPlayer';
import { prefetchNextTrack, resolveSource, takeNextPresign } from './webPresign';

const MEDIA_ERR_NETWORK = 2;
const MEDIA_ERR_DECODE = 3;
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;
const MEDIA_ERROR_KINDS: ReadonlyMap<number, PlaybackErrorKind> = new Map([
  [MEDIA_ERR_NETWORK, 'network'],
  [MEDIA_ERR_DECODE, 'decode'],
]);
const UNPLAYABLE_AUDIO = 'The audio could not be played';

const RECOVERABLE_ERROR_CODES: ReadonlySet<number> = new Set([
  MEDIA_ERR_NETWORK,
  MEDIA_ERR_SRC_NOT_SUPPORTED,
]);

function mediaFailure(error: MediaError | null): RedactedPlaybackFailure {
  const kind = MEDIA_ERROR_KINDS.get(error?.code ?? 0) ?? 'unknown';
  const detail = error?.message ?? '';
  return { kind, message: redactPlaybackErrorMessage(detail === '' ? UNPLAYABLE_AUDIO : detail) };
}

function markAwaitingSource(player: WebAudioPlayer): void {
  player.awaitingSource = true;
  syncPhase(player);
}

export async function represignAndResume(player: WebAudioPlayer, opts: LoadOptions): Promise<void> {
  const track = player.lastTrack;
  if (player.awaitingSource || !track) return;
  const seq = player.loadSeq;
  markAwaitingSource(player);
  const outcome = await resolveSource(track.source);
  if (seq === player.loadSeq) applySource(player, outcome, opts);
}

function isRecoverableMediaError(error: MediaError | null): boolean {
  return RECOVERABLE_ERROR_CODES.has(error?.code ?? -1);
}

function recoverFromMediaError(player: WebAudioPlayer): Promise<void> {
  const startPositionMs = secondsToMs(player.audio.currentTime);
  return represignAndResume(player, { autoplay: true, startPositionMs });
}

function surfaceFailure(player: WebAudioPlayer, failure: RedactedPlaybackFailure): void {
  recordPlaybackFailure(failure.kind);
  player.update({ failure });
}

function canRecoverFromMediaError(player: WebAudioPlayer): boolean {
  return (
    !player.recoveryAttempted &&
    isRecoverableMediaError(player.audio.error) &&
    isSourceStale(player)
  );
}

export function reportMediaError(player: WebAudioPlayer): void {
  if (player.awaitingSource) return;
  if (canRecoverFromMediaError(player)) {
    player.recoveryAttempted = true;
    void recoverFromMediaError(player);
    return;
  }
  surfaceFailure(player, mediaFailure(player.audio.error));
}

export function markRecovered(player: WebAudioPlayer): void {
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

export function advanceOnEnded(player: WebAudioPlayer): void {
  syncPhase(player);
  const { repeatMode } = useQueueStore.getState();
  if (repeatMode === 'one') {
    replayCurrentTrack(player);
    return;
  }
  void loadIfPresent(player, useQueueStore.getState().skipToNext());
}

function releaseSource(audio: HTMLAudioElement): void {
  audio.pause();
  audio.removeAttribute('src');
  audio.load();
}

export function playAudio(player: WebAudioPlayer): void {
  void player.audio.play().catch((err: unknown) => {
    warnPlayback('web play() rejected', {}, err);
    syncPhase(player);
  });
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

function applyResolvedSource(
  player: WebAudioPlayer,
  url: string,
  options: LoadOptions,
  issuedAt?: number,
): void {
  player.sourceIssuedAt = issuedAt ?? player.now();
  startSource(player, url, options);
}

function applySource(
  player: WebAudioPlayer,
  outcome: SourceOutcome,
  options: LoadOptions,
  issuedAt?: number,
): void {
  player.awaitingSource = false;
  if ('url' in outcome) applyResolvedSource(player, outcome.url, options, issuedAt);
  else surfaceFailure(player, outcome.failure);
}

export async function loadTrack(
  player: WebAudioPlayer,
  track: PlaybackTrack,
  options: LoadOptions = {},
): Promise<void> {
  const seq = beginLoad(player, track);
  const cached = takeNextPresign(player, track);
  const outcome = cached ? { url: cached.url } : await resolveSource(track.source);
  if (seq === player.loadSeq) applySource(player, outcome, options, cached?.issuedAt);
}

export async function loadIfPresent(
  player: WebAudioPlayer,
  track: PlaybackTrack | null | undefined,
  options?: LoadOptions,
): Promise<void> {
  if (track) await loadTrack(player, track, options);
}

export function stopPlayer(player: WebAudioPlayer): void {
  player.loadSeq += 1;
  player.awaitingSource = false;
  player.lastTrack = null;
  player.sourceIssuedAt = null;
  player.recoveryAttempted = false;
  player.nextPresign = null;
  releaseSource(player.audio);
  player.update(IDLE_PLAYBACK);
}

export function endForSignOut(player: WebAudioPlayer): void {
  stopPlayer(player);
  useQueueStore.getState().clearQueue();
}
