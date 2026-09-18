import { create } from 'zustand';

import { ApiError, NetworkError } from '@shared/api-client/errors';
import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

// Native player errors (ExoPlayer/AVFoundation) often embed the failing request: a
// presigned stream URL, its query-string signature, or the bearer header. The message
// is shown on screen, so every report is scrubbed here, once, before it is stored.
// Capped first so a pathological native string cannot make the patterns expensive.
const MAX_MESSAGE_LENGTH = 500;
const REDACTED = '[redacted]';
const REDACTIONS: readonly [RegExp, string][] = [
  [/\b[a-z][a-z0-9+.-]*:\/\/[^\s"'<>]+/gi, '[redacted url]'],
  [/\beyJ[\w-]*\.[\w-]+\.[\w-]*/g, REDACTED],
  [/\b(bearer|basic)\s+[\w.~+/=-]+/gi, `$1 ${REDACTED}`],
  [/\?[^\s?#"'<>]*=[^\s"'<>]*/g, `?${REDACTED}`],
  [
    /\b([\w-]*(?:token|signature|secret|password|credential|authorization|session|api[-_]?key)[\w-]*)(\s*[=:]\s*)[^\s&"',;]+/gi,
    `$1$2${REDACTED}`,
  ],
];

function redactPlaybackErrorMessage(message: string): string {
  let redacted = message.slice(0, MAX_MESSAGE_LENGTH);
  for (const [pattern, replacement] of REDACTIONS) {
    redacted = redacted.replace(pattern, replacement);
  }
  return redacted;
}

/**
 * What kind of failure a stored error is, so callers branch on this, never on `message`.
 * - `network`: no connection, a timeout, or a server-side (5xx/429) failure; may succeed later.
 * - `auth`: the stream request was refused (401/403), e.g. an expired signed URL or session.
 * - `not_found`: the audio no longer exists (404/410, missing file).
 * - `decode`: the audio arrived but cannot be parsed or decoded.
 * - `queue_out_of_sync` / `queue_update_failed`: a native queue mutation failed (permanent
 *   drift vs a transient failure), see `createNativePlaybackActions`.
 * - `unknown`: anything the native layer or loader does not let us tell apart.
 */
export type PlaybackErrorKind =
  | 'network'
  | 'auth'
  | 'not_found'
  | 'decode'
  | 'queue_out_of_sync'
  | 'queue_update_failed'
  | 'unknown';

function httpStatusKind(status: number): PlaybackErrorKind {
  if (status === 401 || status === 403) return 'auth';
  if (status === 404 || status === 410) return 'not_found';
  if (status === 408 || status === 429 || status >= 500) return 'network';
  return 'unknown';
}

// ExoPlayer's InvalidResponseCodeException message is "Response code: <status>".
const HTTP_STATUS_IN_MESSAGE = /\bresponse code:?\s*(\d{3})\b/i;

const NETWORK_NATIVE_CODES: ReadonlySet<string> = new Set([
  'android-io-network-connection-failed',
  'android-io-network-connection-timeout',
  'android-timeout',
  'ios_not_connected_to_internet',
]);
const NOT_FOUND_NATIVE_CODES: ReadonlySet<string> = new Set(['android-io-file-not-found']);
const DECODE_NATIVE_CODES: ReadonlySet<string> = new Set(['ios_track_unplayable']);
const DECODE_NATIVE_PREFIXES = ['android-parsing-', 'android-decoding-'];

/**
 * Classifies a react-native-track-player `PlaybackError` event. Android codes are
 * ExoPlayer's error code names (`android-io-bad-http-status`, `android-decoding-failed`),
 * iOS codes come from the player's own mapping (`ios_not_connected_to_internet`).
 * Runs on the raw message, before redaction; only the HTTP status is read from it.
 */
export function classifyNativePlaybackError(code: string, message: string): PlaybackErrorKind {
  if (code === 'android-io-bad-http-status') {
    const status = HTTP_STATUS_IN_MESSAGE.exec(message)?.[1];
    return status ? httpStatusKind(Number(status)) : 'network';
  }
  if (NETWORK_NATIVE_CODES.has(code)) return 'network';
  if (NOT_FOUND_NATIVE_CODES.has(code)) return 'not_found';
  if (DECODE_NATIVE_CODES.has(code) || DECODE_NATIVE_PREFIXES.some((p) => code.startsWith(p))) {
    return 'decode';
  }
  return 'unknown';
}

/** Classifies a rejected load: an API or transport error, or a native rejection with a code. */
export function classifyPlaybackFailure(err: unknown): PlaybackErrorKind {
  if (err instanceof NetworkError) return 'network';
  if (err instanceof ApiError) return httpStatusKind(err.status);
  if (typeof err !== 'object' || err === null || !('code' in err)) return 'unknown';
  if (typeof err.code !== 'string') return 'unknown';
  const message = err instanceof Error ? err.message : '';
  return classifyNativePlaybackError(err.code, message);
}

interface PlaybackErrorState {
  key: TrackKey | null;
  kind: PlaybackErrorKind | null;
  message: string | null;
  report: (key: TrackKey, kind: PlaybackErrorKind, message: string) => void;
  clear: () => void;
}

export const usePlaybackErrorStore = create<PlaybackErrorState>((set) => ({
  key: null,
  kind: null,
  message: null,
  report: (key, kind, message) => set({ key, kind, message: redactPlaybackErrorMessage(message) }),
  clear: () => set({ key: null, kind: null, message: null }),
}));

export function reportPlaybackError(key: TrackKey, kind: PlaybackErrorKind, message: string): void {
  usePlaybackErrorStore.getState().report(key, kind, message);
}

function loadFailureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Failed to load audio';
}

/**
 * The one way a failed load is surfaced: classified, keyed to the track that failed.
 * `message` defaults to the rejection's own text, for callers with nothing better to show.
 */
export function reportLoadFailure(
  track: PlaybackTrack,
  err: unknown,
  message: string = loadFailureMessage(err),
): void {
  reportPlaybackError(trackKey(track), classifyPlaybackFailure(err), message);
}

export function clearPlaybackError(): void {
  usePlaybackErrorStore.getState().clear();
}

export function usePlaybackErrorFor(key: TrackKey | null): string | null {
  return usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));
}
