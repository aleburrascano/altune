import type { PlaybackErrorKind } from '@shared/playback/types';

import { classifyPlaybackFailure } from './classifyPlaybackError';

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

export function redactPlaybackErrorMessage(message: string): string {
  let redacted = message.slice(0, MAX_MESSAGE_LENGTH);
  for (const [pattern, replacement] of REDACTIONS) {
    redacted = redacted.replace(pattern, replacement);
  }
  return redacted;
}

export interface RedactedPlaybackFailure {
  readonly kind: PlaybackErrorKind;
  readonly message: string;
}

function failureText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return typeof err === 'string' ? err : `non-Error rejection: ${typeof err}`;
}

export function redactedPlaybackFailure(err: unknown): RedactedPlaybackFailure {
  return {
    kind: classifyPlaybackFailure(err),
    message: redactPlaybackErrorMessage(failureText(err)),
  };
}
