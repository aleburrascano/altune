import fc from 'fast-check';

import * as SessionNamespace from '../session';

type AppStateChangeHandler = (state: string) => void;

jest.mock('react-native/Libraries/AppState/AppState', () => {
  const listeners: AppStateChangeHandler[] = [];
  return {
    default: {
      currentState: 'active',
      isAvailable: true,
      addEventListener: jest.fn((type: string, handler: AppStateChangeHandler) => {
        if (type === 'change') listeners.push(handler);
        return { remove: jest.fn() };
      }),
    },
    __listeners: listeners,
  };
});

const { advanceSession, makeSessionId, SESSION_INACTIVITY_MS } = SessionNamespace;
type SessionState = SessionNamespace.SessionState;

type MockAppStateModule = {
  default: { addEventListener: jest.Mock; currentState: string };
  __listeners: AppStateChangeHandler[];
};

// `set` moves real time forward: the wall clock and the monotonic tick advance
// together. `jumpWall` moves only the wall clock, as an NTP resync or a manual
// clock change does, while `tick` keeps measuring real elapsed time.
function fakeClock(initialNow: number) {
  let wall = initialNow;
  let tick = initialNow;
  const now = () => wall;
  const monotonic = () => tick;
  const set = (value: number) => {
    tick += value - wall;
    wall = value;
  };
  const jumpWall = (value: number) => {
    wall = value;
  };
  return { now, monotonic, set, jumpWall };
}

// resetModules only buys a fresh singleton (_state, _listening, the tick
// anchor); time comes from the injected clocks, never from patching globals.
function loadFreshSession(initialNow: number) {
  jest.resetModules();
  const rawSession: typeof SessionNamespace = require('../session');
  const clock = fakeClock(initialNow);
  const session = { getSessionId: () => rawSession.getSessionId(clock.now, clock.monotonic) };

  const appState: MockAppStateModule = require('react-native/Libraries/AppState/AppState');
  appState.default.currentState = 'active';
  // The module-load id is seeded from the real clock; one read re-anchors it to the fake one.
  session.getSessionId();
  return { session, appState, clock };
}

// Mirrors react-native, which updates AppState.currentState before notifying listeners.
function emit(appState: MockAppStateModule, status: string): void {
  appState.default.currentState = status;
  [...appState.__listeners].forEach((handler) => handler(status));
}

afterEach(() => {
  jest.restoreAllMocks();
});

describe('advanceSession — the inactivity boundary at SESSION_INACTIVITY_MS', () => {
  const state: SessionState = { sessionId: 'base-session', lastActivity: 1_000_000 };

  it.each([
    ['one ms under the threshold keeps the session', SESSION_INACTIVITY_MS - 1, false],
    ['exactly at the threshold keeps the session (> not >=)', SESSION_INACTIVITY_MS, false],
    ['one ms over the threshold rotates the session', SESSION_INACTIVITY_MS + 1, true],
    ['a zero-length gap keeps the session', 0, false],
    ['a clock that moved backwards keeps the session', -1000, false],
  ])('%s', (_label, gap, shouldRotate) => {
    const now = state.lastActivity + gap;
    const result = advanceSession(state, now);
    expect(result.lastActivity).toBe(now);
    if (shouldRotate) {
      expect(result.sessionId).not.toBe(state.sessionId);
    } else {
      expect(result.sessionId).toBe(state.sessionId);
    }
  });
});

describe('advanceSession — reducer arms', () => {
  it('keep arm returns a new object with the same sessionId but an advanced lastActivity', () => {
    const state: SessionState = { sessionId: 'stable-id', lastActivity: 0 };

    const result = advanceSession(state, 1000);

    expect(result).not.toBe(state);
    expect(result.sessionId).toBe(state.sessionId);
    expect(result.lastActivity).toBe(1000);
  });

  it('rotate arm returns a fresh sessionId alongside the new lastActivity', () => {
    const state: SessionState = { sessionId: 'stale-id', lastActivity: 0 };
    const now = SESSION_INACTIVITY_MS + 1;

    const result = advanceSession(state, now);

    expect(result.sessionId).not.toBe(state.sessionId);
    expect(result.lastActivity).toBe(now);
  });

  it('does not mutate the state object it was given, in either arm', () => {
    const keepState: SessionState = { sessionId: 'id-a', lastActivity: 0 };
    const keepSnapshot = { ...keepState };
    advanceSession(keepState, 1000);
    expect(keepState).toEqual(keepSnapshot);

    const rotateState: SessionState = { sessionId: 'id-b', lastActivity: 0 };
    const rotateSnapshot = { ...rotateState };
    advanceSession(rotateState, SESSION_INACTIVITY_MS + 1);
    expect(rotateState).toEqual(rotateSnapshot);
  });
});

describe('law: advanceSession(s, now).lastActivity === now for every input', () => {
  it('holds regardless of whether the session rotates or is kept', () => {
    fc.assert(
      fc.property(
        fc.record({ sessionId: fc.string(), lastActivity: fc.integer() }),
        fc.integer(),
        (state, now) => {
          expect(advanceSession(state, now).lastActivity).toBe(now);
        },
      ),
      { numRuns: 300 },
    );
  });
});

describe('law: the sessionId rotates if and only if now - lastActivity > SESSION_INACTIVITY_MS', () => {
  it('holds across generated states and clock values', () => {
    fc.assert(
      fc.property(
        fc.record({ sessionId: fc.string({ minLength: 1 }), lastActivity: fc.integer() }),
        fc.integer(),
        (state, now) => {
          const result = advanceSession(state, now);
          const rotated = result.sessionId !== state.sessionId;
          expect(rotated).toBe(now - state.lastActivity > SESSION_INACTIVITY_MS);
        },
      ),
      { numRuns: 300 },
    );
  });
});

describe('law: advanceSession never mutates the state it was given', () => {
  it('leaves the input object unchanged for every generated state/now pair', () => {
    fc.assert(
      fc.property(
        fc.record({ sessionId: fc.string(), lastActivity: fc.integer() }),
        fc.integer(),
        (state, now) => {
          const before = { ...state };
          advanceSession(state, now);
          expect(state).toEqual(before);
        },
      ),
      { numRuns: 300 },
    );
  });
});

describe('law: repeated application within the window is stable', () => {
  it('chaining any number of in-window advances never rotates the sessionId', () => {
    fc.assert(
      fc.property(
        fc.string({ minLength: 1 }),
        fc.integer({ min: 0, max: 1e9 }),
        fc.array(fc.integer({ min: 0, max: SESSION_INACTIVITY_MS }), { minLength: 1, maxLength: 20 }),
        (sessionId, startLastActivity, deltas) => {
          let state: SessionState = { sessionId, lastActivity: startLastActivity };
          for (const delta of deltas) {
            state = advanceSession(state, state.lastActivity + delta);
          }
          expect(state.sessionId).toBe(sessionId);
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: makeSessionId(seed) prefixes the base36 seed and separator, and varies per call', () => {
  it('holds for generated seeds, and two calls with the same seed differ', () => {
    fc.assert(
      fc.property(fc.integer({ min: 0, max: Number.MAX_SAFE_INTEGER }), (seed) => {
        const first = makeSessionId(seed);
        expect(first.startsWith(`${seed.toString(36)}-`)).toBe(true);

        const second = makeSessionId(seed);
        expect(second).not.toBe(first);
      }),
      { numRuns: 200 },
    );
  });
});

describe('getSessionId — the AppState listener is registered exactly once', () => {
  it('addEventListener is called once no matter how many times getSessionId() runs', () => {
    const { session, appState } = loadFreshSession(1000);

    session.getSessionId();
    session.getSessionId();
    session.getSessionId();

    expect(appState.default.addEventListener).toHaveBeenCalledTimes(1);
  });
});

describe('the foreground listener rotates only after the inactivity window', () => {
  it('keeps the session across a one-second background/foreground cycle', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(2000);
    emit(appState, 'active');

    const after = session.getSessionId();
    expect(after).toBe(before);
  });

  it('keeps the session when returning just inside the window', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS);
    emit(appState, 'active');

    expect(session.getSessionId()).toBe(before);
  });

  it('rotates on return once the background period exceeded the window', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    emit(appState, 'active');

    expect(session.getSessionId()).not.toBe(before);
  });

  it('measures the background gap from the last activity, even with no read after returning', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    emit(appState, 'active');
    const rotated = session.getSessionId();
    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS + 2000);
    emit(appState, 'active');

    expect(rotated).not.toBe(before);
    expect(session.getSessionId()).toBe(rotated);
  });

  it('does not rotate on a transition to background or inactive', () => {
    const { session, appState } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    emit(appState, 'inactive');

    const after = session.getSessionId();
    expect(after).toBe(before);
  });
});

describe('getSessionId rotates on inactivity alone, with no AppState transition', () => {
  it('returns a new id once the inactivity window has elapsed between two reads', () => {
    const { session, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    const after = session.getSessionId();

    expect(after).not.toBe(before);
  });

  it('returns the same id when the reads fall inside the window, however many there are', () => {
    const { session, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    for (let i = 1; i <= 4; i += 1) {
      clock.set(1000 + (SESSION_INACTIVITY_MS - 1) * i);
      expect(session.getSessionId()).toBe(before);
    }
  });

  it('treats each read as activity, so steady polling inside the window never rotates', () => {
    const { session, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    clock.set(1000 + SESSION_INACTIVITY_MS);
    session.getSessionId();
    clock.set(1000 + SESSION_INACTIVITY_MS * 2);
    const after = session.getSessionId();

    expect(after).toBe(before);
  });
});

describe('every id this module hands out is a well-formed session id, never a degenerate value', () => {
  const SESSION_ID_SHAPE = /^[0-9a-z]+-[0-9a-z]{1,8}$/;

  it('makeSessionId produces a base36 seed, one separator, and a base36 suffix with nothing else in it', () => {
    for (const seed of [0, 1, 1000, Date.now()]) {
      const id = makeSessionId(seed);
      expect(id).toMatch(SESSION_ID_SHAPE);
      expect(id.startsWith(`${seed.toString(36)}-`)).toBe(true);
    }
  });

  it('the id a foreground rotation installs is a well-formed string, not an empty or absent one', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    emit(appState, 'active');
    const rotated = session.getSessionId();

    expect(rotated).not.toBe(before);
    expect(typeof rotated).toBe('string');
    expect(rotated).toMatch(SESSION_ID_SHAPE);
  });

  it('the id survives an inactivity rotation as a well-formed string', () => {
    const { session, clock } = loadFreshSession(1000);
    session.getSessionId();

    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    const rotated = session.getSessionId();

    expect(rotated).toMatch(SESSION_ID_SHAPE);
  });
});

describe('the inactivity window is the product value, not an arbitrary one', () => {
  it('is 30 minutes', () => {
    expect(SESSION_INACTIVITY_MS).toBe(30 * 60 * 1000);
  });
});

describe('ordering — a foreground rotation is visible to the very next getSessionId() call', () => {
  it('returns the id the listener just rotated to, not a value from before the rotation', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.set(1000 + SESSION_INACTIVITY_MS + 1);
    emit(appState, 'active');
    const rotatedOnce = session.getSessionId();
    const rotatedAgain = session.getSessionId();

    expect(rotatedOnce).not.toBe(before);
    expect(rotatedAgain).toBe(rotatedOnce);
  });
});

describe('a wall-clock jump neither triggers nor suppresses a rotation', () => {
  it('keeps the session when the wall clock leaps past the window while foregrounded', () => {
    const { session, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    clock.jumpWall(1000 + 2 * 60 * 60 * 1000);

    expect(session.getSessionId()).toBe(before);
  });

  it('still rotates on real idle time after the wall clock jumped backwards while foregrounded', () => {
    const { session, clock } = loadFreshSession(10 * SESSION_INACTIVITY_MS);
    const before = session.getSessionId();

    clock.jumpWall(0);
    expect(session.getSessionId()).toBe(before);
    clock.set(SESSION_INACTIVITY_MS + 1);

    expect(session.getSessionId()).not.toBe(before);
  });

  it('does not let a foreground wall jump leak into the next background gap', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    clock.jumpWall(1000 + 2 * 60 * 60 * 1000);
    session.getSessionId();
    emit(appState, 'background');
    clock.set(1000 + 2 * 60 * 60 * 1000 + 1000);
    emit(appState, 'active');

    expect(session.getSessionId()).toBe(before);
  });

  it('keeps the session when the wall clock moved backwards across a background period', () => {
    const { session, appState, clock } = loadFreshSession(10 * SESSION_INACTIVITY_MS);
    const before = session.getSessionId();

    emit(appState, 'background');
    clock.jumpWall(1000);
    emit(appState, 'active');

    expect(session.getSessionId()).toBe(before);
  });

  it('falls back to the wall clock for reads made while backgrounded', () => {
    const { session, appState, clock } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    session.getSessionId();
    clock.jumpWall(1000 + SESSION_INACTIVITY_MS + 1);

    expect(session.getSessionId()).not.toBe(before);
  });
});
