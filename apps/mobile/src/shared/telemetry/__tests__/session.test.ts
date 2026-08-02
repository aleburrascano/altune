import fc from 'fast-check';

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

import * as SessionNamespace from '../session';

const { advanceSession, makeSessionId, SESSION_INACTIVITY_MS } = SessionNamespace;
type SessionState = SessionNamespace.SessionState;

type MockAppStateModule = {
  default: { addEventListener: jest.Mock };
  __listeners: AppStateChangeHandler[];
};

function loadFreshSession(initialNow: number) {
  jest.resetModules();
  const dateNowSpy = jest.spyOn(Date, 'now').mockReturnValue(initialNow);
  const session: typeof SessionNamespace = require('../session');

  const appState: MockAppStateModule = require('react-native/Libraries/AppState/AppState');
  return { session, appState, dateNowSpy };
}

function emit(appState: MockAppStateModule, status: string): void {
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

describe('the foreground listener rotates unconditionally on active', () => {
  it('rotates after only a one-second backgrounding, a gap advanceSession alone would keep', () => {
    const { session, appState, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    emit(appState, 'background');
    dateNowSpy.mockReturnValue(2000);
    emit(appState, 'active');

    const after = session.getSessionId();
    expect(after).not.toBe(before);
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
    const { session, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    dateNowSpy.mockReturnValue(1000 + SESSION_INACTIVITY_MS + 1);
    const after = session.getSessionId();

    expect(after).not.toBe(before);
  });

  it('returns the same id when the reads fall inside the window, however many there are', () => {
    const { session, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    for (let i = 1; i <= 4; i += 1) {
      dateNowSpy.mockReturnValue(1000 + (SESSION_INACTIVITY_MS - 1) * i);
      expect(session.getSessionId()).toBe(before);
    }
  });

  it('treats each read as activity, so steady polling inside the window never rotates', () => {
    const { session, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    dateNowSpy.mockReturnValue(1000 + SESSION_INACTIVITY_MS);
    session.getSessionId();
    dateNowSpy.mockReturnValue(1000 + SESSION_INACTIVITY_MS * 2);
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
    const { session, appState, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    dateNowSpy.mockReturnValue(2000);
    emit(appState, 'active');
    const rotated = session.getSessionId();

    expect(rotated).not.toBe(before);
    expect(typeof rotated).toBe('string');
    expect(rotated).toMatch(SESSION_ID_SHAPE);
  });

  it('the id survives an inactivity rotation as a well-formed string', () => {
    const { session, dateNowSpy } = loadFreshSession(1000);
    session.getSessionId();

    dateNowSpy.mockReturnValue(1000 + SESSION_INACTIVITY_MS + 1);
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
    const { session, appState, dateNowSpy } = loadFreshSession(1000);
    const before = session.getSessionId();

    dateNowSpy.mockReturnValue(2000);
    emit(appState, 'active');
    const rotatedOnce = session.getSessionId();
    const rotatedAgain = session.getSessionId();

    expect(rotatedOnce).not.toBe(before);
    expect(rotatedAgain).toBe(rotatedOnce);
  });
});
