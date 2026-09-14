import { create } from 'zustand';

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

interface PlaybackErrorState {
  key: string | null;
  message: string | null;
  report: (key: string, message: string) => void;
  clear: () => void;
}

export const usePlaybackErrorStore = create<PlaybackErrorState>((set) => ({
  key: null,
  message: null,
  report: (key, message) => set({ key, message: redactPlaybackErrorMessage(message) }),
  clear: () => set({ key: null, message: null }),
}));

export function reportPlaybackError(key: string, message: string): void {
  usePlaybackErrorStore.getState().report(key, message);
}

export function clearPlaybackError(): void {
  usePlaybackErrorStore.getState().clear();
}

export function usePlaybackErrorFor(key: string | null): string | null {
  return usePlaybackErrorStore((s) => (key != null && s.key === key ? s.message : null));
}
