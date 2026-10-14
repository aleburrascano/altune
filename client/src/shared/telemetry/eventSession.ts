import { isAppActive, subscribeAppState } from '@shared/lifecycle/appState';

import { onSignOut } from '@shared/session/signOutCleanup';

const SESSION_INACTIVITY_MS = 1_800_000;

type SessionState = { sessionId: string; lastActivity: number };

export type Clock = () => number;

function makeSessionId(seed: number): string {
  return `${seed.toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function advanceSession(state: SessionState, now: number): SessionState {
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
  _tickAnchor = isAppActive() ? at : null;
}

function ensureForegroundRotation(now: Clock, tick: Clock): void {
  if (_listening) return;
  _listening = true;
  subscribeAppState((status) => {
    _tickAnchor = null;
    if (status === 'active') touch(now, tick);
  });
}

export function getSessionId(now: Clock = Date.now, tick: Clock = () => performance.now()): string {
  ensureForegroundRotation(now, tick);
  touch(now, tick);
  return _state.sessionId;
}

function resetSession(now: Clock = Date.now): void {
  const at = now();
  _state = { sessionId: makeSessionId(at), lastActivity: at };
  _tickAnchor = null;
}

onSignOut(() => resetSession());
