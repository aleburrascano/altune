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
// Monotonic reading taken at the last activity, or null once the app has left
// the foreground since then (a suspended process may stop the monotonic clock).
let _tickAnchor: number | null = null;

// Idle time since the last activity that a wall-clock jump cannot fake. While the
// app stayed active the monotonic tick is authoritative; across a background
// period only the wall clock survives, and a backwards jump counts as no time.
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

// Both clocks are injectable so tests drive the singleton with fakes instead of
// patching globals. The clocks given on the first call also back the foreground
// listener, which is registered only once.
export function getSessionId(now: Clock = Date.now, tick: Clock = () => performance.now()): string {
  ensureForegroundRotation(now, tick);
  touch(now, tick);
  return _state.sessionId;
}
