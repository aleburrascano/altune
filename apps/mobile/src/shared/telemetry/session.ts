import { AppState } from 'react-native';

export const SESSION_INACTIVITY_MS = 30 * 60 * 1000;

export type SessionState = { sessionId: string; lastActivity: number };

export function makeSessionId(seed: number): string {
  return `${seed.toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export function advanceSession(state: SessionState, now: number): SessionState {
  if (now - state.lastActivity > SESSION_INACTIVITY_MS) {
    return { sessionId: makeSessionId(now), lastActivity: now };
  }
  return { sessionId: state.sessionId, lastActivity: now };
}

let _state: SessionState = { sessionId: makeSessionId(Date.now()), lastActivity: Date.now() };
let _listening = false;

function ensureForegroundRotation(now: () => number): void {
  if (_listening) return;
  _listening = true;
  AppState.addEventListener('change', (status) => {
    if (status === 'active') {
      const at = now();
      _state = { sessionId: makeSessionId(at), lastActivity: at };
    }
  });
}

// `now` is injectable so tests drive the singleton with a fake clock instead of
// patching Date.now globally. The clock given on the first call also backs the
// foreground listener, which is registered only once.
export function getSessionId(now: () => number = Date.now): string {
  ensureForegroundRotation(now);
  _state = advanceSession(_state, now());
  return _state.sessionId;
}
