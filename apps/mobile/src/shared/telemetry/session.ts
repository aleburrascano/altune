/**
 * session — a rotating session_id stamped onto every behavioral event.
 *
 * The session is the unit of a search arc (search → click → play). Joachims
 * query-chains: reformulation/abandonment within a session reveals what the user
 * meant. The id rotates on app foreground OR after 30 minutes of inactivity, so a
 * fresh app open or a return after a long gap starts a new arc.
 *
 * session_id rides in the JSONB event payload (no DB column — per the
 * "collect richly, model lazily" rule). The pure advanceSession helper holds the
 * inactivity logic and is unit-tested; the stateful wrapper is a thin shell.
 */

import { AppState } from 'react-native';

export const SESSION_INACTIVITY_MS = 30 * 60 * 1000;

export type SessionState = { sessionId: string; lastActivity: number };

export function makeSessionId(seed: number): string {
  return `${seed.toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

// advanceSession returns the state to use for an event at `now`, rotating the id
// after 30 minutes of inactivity. Pure — foreground rotation is handled by the
// AppState listener, which forces a fresh id regardless of inactivity.
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
