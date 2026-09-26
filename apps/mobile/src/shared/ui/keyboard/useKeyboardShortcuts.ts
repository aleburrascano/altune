import { useEffect } from 'react';
import { useRouter, type ImperativeRouter } from 'expo-router';

const SEEK_STEP_MS = 10_000;
const DISCOVER_ROUTE = '/discover';
export const FOCUS_REQUEST_TTL_MS = 5_000;

let registeredFocus: (() => void) | null = null;
let pendingFocusTimer: ReturnType<typeof setTimeout> | null = null;

function clearPendingFocusRequest(): void {
  if (pendingFocusTimer === null) return;
  clearTimeout(pendingFocusTimer);
  pendingFocusTimer = null;
}

export function registerSearchFocus(focus: () => void): () => void {
  registeredFocus = focus;
  if (pendingFocusTimer !== null) {
    clearPendingFocusRequest();
    focus();
  }
  return () => {
    if (registeredFocus === focus) registeredFocus = null;
  };
}

function pushToDiscover(router: ImperativeRouter): void {
  try {
    router.push(DISCOVER_ROUTE);
  } catch {
    clearPendingFocusRequest();
  }
}

function focusDiscoverSearch(router: ImperativeRouter): void {
  if (registeredFocus) {
    registeredFocus();
    return;
  }
  if (pendingFocusTimer !== null) return;
  pendingFocusTimer = setTimeout(clearPendingFocusRequest, FOCUS_REQUEST_TTL_MS);
  pushToDiscover(router);
}

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
  if (event.key === 'ArrowLeft') return () => void playback.skipPrevious();
  if (event.key === 'ArrowRight') return () => void playback.skipNext();
  return null;
}

type Binding = (() => void) | null;

function unshiftedBinding(event: KeyboardEvent, playback: ShortcutPlayback, focusSearch: () => void): Binding {
  if (event.key === ' ') return () => togglePlayback(playback);
  if (event.key === 'ArrowLeft') return () => seekBy(playback, -SEEK_STEP_MS);
  if (event.key === 'ArrowRight') return () => seekBy(playback, SEEK_STEP_MS);
  if (event.key === '/') return focusSearch;
  return null;
}

function bindingFor(event: KeyboardEvent, playback: ShortcutPlayback, focusSearch: () => void): Binding {
  return event.shiftKey
    ? shiftedBinding(event, playback)
    : unshiftedBinding(event, playback, focusSearch);
}

function handleKeyboardEvent(event: KeyboardEvent, playback: ShortcutPlayback, focusSearch: () => void): void {
  if (isTypingTarget(event.target) || hasBrowserModifier(event)) return;
  const action = bindingFor(event, playback, focusSearch);
  if (!action) return;
  event.preventDefault();
  action();
}

function hasAddEventListener(win: Window): boolean {
  return typeof win.addEventListener === 'function';
}

function resolveTarget(target: Window | undefined): Window | null {
  const candidate = target ?? (typeof window === 'undefined' ? null : window);
  if (candidate === null) return null;
  return hasAddEventListener(candidate) ? candidate : null;
}

function attachKeyboardListener(
  win: Window,
  playback: ShortcutPlayback,
  router: ImperativeRouter,
): () => void {
  const onKeyDown = (event: KeyboardEvent) =>
    handleKeyboardEvent(event, playback, () => focusDiscoverSearch(router));
  win.addEventListener('keydown', onKeyDown);
  return () => win.removeEventListener('keydown', onKeyDown);
}

export function useKeyboardShortcuts(playback: ShortcutPlayback, target?: Window): void {
  const router = useRouter();
  useEffect(() => {
    const win = resolveTarget(target);
    return win ? attachKeyboardListener(win, playback, router) : undefined;
  }, [target, playback, router]);
}
