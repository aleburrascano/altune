import type { Dispatch, SetStateAction } from 'react';

import type { TrackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import type { RedactedPlaybackFailure } from '../redactPlaybackError';

type AudioPhase = 'loading' | 'playing' | 'paused' | 'ended';

export interface WebPlayback {
  readonly track: PlaybackTrack | null;
  readonly phase: AudioPhase;
  readonly failure: RedactedPlaybackFailure | null;
  readonly positionMs: number;
  readonly durationMs: number;
}

export interface PendingPresign {
  readonly key: TrackKey;
  readonly url: string;
  readonly issuedAt: number;
}

export interface WebAudioPlayer {
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

export interface LoadOptions {
  autoplay?: boolean;
  startPositionMs?: number;
}

export type SourceOutcome =
  { readonly url: string } | { readonly failure: RedactedPlaybackFailure };

export const IDLE_PLAYBACK: WebPlayback = {
  track: null,
  phase: 'paused',
  failure: null,
  positionMs: 0,
  durationMs: 0,
};

const HAVE_FUTURE_DATA = 3;

const PRESIGN_TTL_MS = 60 * 60 * 1000;
const PRESIGN_REFRESH_MARGIN_MS = 60 * 1000;

type PlayerState = Omit<WebAudioPlayer, 'audio' | 'update' | 'now'>;

function initialPlayerState(): PlayerState {
  return {
    loadSeq: 0,
    awaitingSource: false,
    lastTrack: null,
    sourceIssuedAt: null,
    recoveryAttempted: false,
    nextPresign: null,
  };
}

export function createWebAudioPlayer(
  audio: HTMLAudioElement,
  setPlayback: Dispatch<SetStateAction<WebPlayback>>,
  now: () => number,
): WebAudioPlayer {
  const update = (patch: Partial<WebPlayback>) => setPlayback((prev) => ({ ...prev, ...patch }));
  return { audio, update, now, ...initialPlayerState() };
}

export function secondsToMs(seconds: number): number {
  return Number.isFinite(seconds) ? Math.round(seconds * 1000) : 0;
}

function phaseOf(player: WebAudioPlayer): AudioPhase {
  const { audio } = player;
  if (player.awaitingSource) return 'loading';
  if (audio.ended) return 'ended';
  if (audio.paused) return 'paused';
  return audio.readyState < HAVE_FUTURE_DATA ? 'loading' : 'playing';
}

export function syncPhase(player: WebAudioPlayer): void {
  player.update({ phase: phaseOf(player) });
}

export function isPresignStale(issuedAt: number, now: number): boolean {
  return now - issuedAt >= PRESIGN_TTL_MS - PRESIGN_REFRESH_MARGIN_MS;
}

export function isSourceStale(player: WebAudioPlayer): boolean {
  if (player.sourceIssuedAt === null) return false;
  return isPresignStale(player.sourceIssuedAt, player.now());
}
