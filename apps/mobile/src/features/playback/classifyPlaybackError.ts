import { ApiError, NetworkError } from '@shared/errors';
import type { PlaybackErrorKind } from '@shared/playback/types';

function httpStatusKind(status: number): PlaybackErrorKind {
  if (status === 401 || status === 403) return 'auth';
  if (status === 404 || status === 410) return 'not_found';
  if (status === 408 || status === 429 || status >= 500) return 'network';
  return 'unknown';
}

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

function badHttpStatusKind(message: string): PlaybackErrorKind {
  const status = HTTP_STATUS_IN_MESSAGE.exec(message)?.[1];
  return status ? httpStatusKind(Number(status)) : 'network';
}

function isDecodeNativeCode(code: string): boolean {
  return DECODE_NATIVE_CODES.has(code) || DECODE_NATIVE_PREFIXES.some((p) => code.startsWith(p));
}

export function classifyNativePlaybackError(code: string, message: string): PlaybackErrorKind {
  if (code === 'android-io-bad-http-status') return badHttpStatusKind(message);
  if (NETWORK_NATIVE_CODES.has(code)) return 'network';
  if (NOT_FOUND_NATIVE_CODES.has(code)) return 'not_found';
  if (isDecodeNativeCode(code)) return 'decode';
  return 'unknown';
}

export function classifyPlaybackFailure(err: unknown): PlaybackErrorKind {
  if (err instanceof NetworkError) return 'network';
  if (err instanceof ApiError) return httpStatusKind(err.status);
  if (typeof err !== 'object' || err === null || !('code' in err)) return 'unknown';
  if (typeof err.code !== 'string') return 'unknown';
  const message = err instanceof Error ? err.message : '';
  return classifyNativePlaybackError(err.code, message);
}
