import { AppState } from 'react-native';

export const SESSION_INACTIVITY_MS = 30 * 60 * 1000;

export type SessionState = { sessionId: string; lastActivity: number };

export type Clock = () => number;

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
let _tickAnchor: number | null = null;

function trustedElapsed(wall: number, tick: number): number {
  const elapsed = _tickAnchor === null ? wall - _state.lastActivity : tick - _tickAnchor;
  return Math.max(0, elapsed);
}

function touch(now: Clock, tick: Clock): void {
  const wall = now();
  const at = tick();
  const rebased = { ..._state, lastActivity: wall - trustedElapsed(wall, at) };
  _state = advanceSession(rebased, wall);
  _tickAnchor = AppState.currentState === 'active' ? at : null;
}

function ensureForegroundRotation(now: Clock, tick: Clock): void {
  if (_listening) return;
  _listening = true;
  AppState.addEventListener('change', (status) => {
    _tickAnchor = null;
    if (status === 'active') touch(now, tick);
  });
}

export function getSessionId(now: Clock = Date.now, tick: Clock = () => performance.now()): string {
  ensureForegroundRotation(now, tick);
  touch(now, tick);
  return _state.sessionId;
}
