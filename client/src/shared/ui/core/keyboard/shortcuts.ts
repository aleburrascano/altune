const SEEK_STEP_MS = 10_000;

export interface ShortcutPlayback {
  readonly status: 'idle' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';
  readonly positionMs: number;
  pause(): void;
  resume(): void;
  seekTo(positionMs: number): void;
  skipNext(): void;
  skipPrevious(): void;
}

interface MaybeFormField {
  readonly tagName?: string;
  readonly isContentEditable?: boolean;
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (target === null || typeof target !== 'object') return false;
  const field = target as MaybeFormField;
  if (field.isContentEditable === true) return true;
  return field.tagName === 'INPUT' || field.tagName === 'TEXTAREA' || field.tagName === 'SELECT';
}

function hasBrowserModifier(event: KeyboardEvent): boolean {
  return event.ctrlKey || event.metaKey || event.altKey;
}

function togglePlayback(playback: ShortcutPlayback): void {
  if (playback.status === 'playing') playback.pause();
  else playback.resume();
}

function seekBy(playback: ShortcutPlayback, deltaMs: number): void {
  playback.seekTo(Math.max(0, playback.positionMs + deltaMs));
}

function shiftedBinding(event: KeyboardEvent, playback: ShortcutPlayback): (() => void) | null {
  if (event.key === 'ArrowLeft') return () => playback.skipPrevious();
  if (event.key === 'ArrowRight') return () => playback.skipNext();
  return null;
}

type Binding = (() => void) | null;

function unshiftedBinding(
  event: KeyboardEvent,
  playback: ShortcutPlayback,
  focusSearch: () => void,
): Binding {
  if (event.key === ' ') return () => togglePlayback(playback);
  if (event.key === 'ArrowLeft') return () => seekBy(playback, -SEEK_STEP_MS);
  if (event.key === 'ArrowRight') return () => seekBy(playback, SEEK_STEP_MS);
  return event.key === '/' ? focusSearch : null;
}

function bindingFor(
  event: KeyboardEvent,
  playback: ShortcutPlayback,
  focusSearch: () => void,
): Binding {
  return event.shiftKey
    ? shiftedBinding(event, playback)
    : unshiftedBinding(event, playback, focusSearch);
}

export function handleKeyboardEvent(
  event: KeyboardEvent,
  playback: ShortcutPlayback,
  focusSearch: () => void,
): void {
  if (isTypingTarget(event.target) || hasBrowserModifier(event)) return;
  const action = bindingFor(event, playback, focusSearch);
  if (action) event.preventDefault();
  action?.();
}
