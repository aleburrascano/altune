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

function ensureForegroundRotation(): void {
  if (_listening) return;
  _listening = true;
  AppState.addEventListener('change', (status) => {
    if (status === 'active') {
      const now = Date.now();
      _state = { sessionId: makeSessionId(now), lastActivity: now };
    }
  });
}

export function getSessionId(): string {
  ensureForegroundRotation();
  _state = advanceSession(_state, Date.now());
  return _state.sessionId;
}
