import type { TelemetryAppState, TelemetryClock } from '@shared/telemetry/ports/telemetry';

const SESSION_INACTIVITY_MS = 1_800_000;
const ID_RANDOM_RADIX = 36;
const ID_RANDOM_START = 2;
const ID_RANDOM_END = 10;

type SessionState = { sessionId: string; lastActivity: number };

type EventSessionDeps = {
  clock: TelemetryClock;
  appState: TelemetryAppState;
  onSignOut: (listener: () => void) => void;
};

function makeSessionId(seed: number, random: number): string {
  const suffix = random.toString(ID_RANDOM_RADIX).slice(ID_RANDOM_START, ID_RANDOM_END);
  return `${seed.toString(ID_RANDOM_RADIX)}-${suffix}`;
}

export function createEventSession({ clock, appState, onSignOut }: EventSessionDeps) {
  let state: SessionState = {
    sessionId: makeSessionId(clock.now(), clock.random()),
    lastActivity: clock.now(),
  };
  let listening = false;
  let tickAnchor: number | null = null;

  function advance(current: SessionState, now: number): SessionState {
    if (now - current.lastActivity > SESSION_INACTIVITY_MS) {
      return { sessionId: makeSessionId(now, clock.random()), lastActivity: now };
    }
    return { sessionId: current.sessionId, lastActivity: now };
  }

  function trustedElapsed(wall: number, tick: number): number {
    const elapsed = tickAnchor === null ? wall - state.lastActivity : tick - tickAnchor;
    return Math.max(0, elapsed);
  }

  function touch(): void {
    const wall = clock.now();
    const at = clock.tick();
    const rebased = { ...state, lastActivity: wall - trustedElapsed(wall, at) };
    state = advance(rebased, wall);
    tickAnchor = appState.isActive() ? at : null;
  }

  function ensureForegroundRotation(): void {
    if (listening) return;
    listening = true;
    appState.subscribe((status) => {
      tickAnchor = null;
      if (status === 'active') touch();
    });
  }

  onSignOut(() => {
    const at = clock.now();
    state = { sessionId: makeSessionId(at, clock.random()), lastActivity: at };
    tickAnchor = null;
  });

  function getSessionId(): string {
    ensureForegroundRotation();
    touch();
    return state.sessionId;
  }

  return { getSessionId };
}
