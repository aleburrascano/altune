import fc from 'fast-check';

import * as SessionNamespace from '@shared/telemetry/eventSession';

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

const SESSION_INACTIVITY_MS = 1_800_000;
const SESSION_ID_SHAPE = /^[0-9a-z]+-[0-9a-z]{1,8}$/;
const START = 1_000_000;
const LONGEST_GAP = 5_400_000;
const BASE36 = 36;

type MockAppStateModule = {
  default: { addEventListener: jest.Mock; currentState: string };
  __listeners: AppStateChangeHandler[];
};

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

function loadFreshSession(initialNow: number) {
  jest.resetModules();
  const rawSession: typeof SessionNamespace = require('@shared/telemetry/eventSession');
  const clock = fakeClock(initialNow);
  const session = { getSessionId: () => rawSession.getSessionId(clock.now, clock.monotonic) };

  const appState: MockAppStateModule = require('react-native/Libraries/AppState/AppState');
  appState.default.currentState = 'active';
  session.getSessionId();
  return { session, appState, clock };
}

function emit(appState: MockAppStateModule, status: string): void {
  appState.default.currentState = status;
  [...appState.__listeners].forEach((handler) => handler(status));
}

afterEach(() => {
  jest.restoreAllMocks();
});

describe('getSessionId — the inactivity boundary at SESSION_INACTIVITY_MS', () => {
  it.each([
    ['one ms under the threshold keeps the session', SESSION_INACTIVITY_MS - 1, false],
    ['exactly at the threshold keeps the session (> not >=)', SESSION_INACTIVITY_MS, false],
    ['one ms over the threshold rotates the session', SESSION_INACTIVITY_MS + 1, true],
    ['a zero-length gap keeps the session', 0, false],
    ['a clock that moved backwards keeps the session', -1000, false],
  ])('%s', (_label, gap, shouldRotate) => {
    const { session, clock } = loadFreshSession(START);
    const before = session.getSessionId();

    clock.set(START + gap);

    expect(session.getSessionId() !== before).toBe(shouldRotate);
  });
});

describe('law: the sessionId rotates if and only if the gap since the last read exceeds SESSION_INACTIVITY_MS', () => {
  it('holds across generated sequences of gaps between reads', () => {
    const { session, clock } = loadFreshSession(START);
    let now = START;
    fc.assert(
      fc.property(
        fc.array(fc.integer({ min: -SESSION_INACTIVITY_MS, max: LONGEST_GAP }), {
          minLength: 1,
          maxLength: 20,
        }),
        (gaps) => {
          for (const gap of gaps) {
            const before = session.getSessionId();
            now += gap;
            clock.set(now);
            expect(session.getSessionId() !== before).toBe(gap > SESSION_INACTIVITY_MS);
          }
        },
      ),
      { numRuns: 200 },
    );
  });
});

describe('law: a rotated session id prefixes the base36 time of the rotation, and varies per rotation', () => {
  it('holds for generated rotation times', () => {
    const { session, clock } = loadFreshSession(0);
    let now = 0;
    fc.assert(
      fc.property(fc.integer({ min: SESSION_INACTIVITY_MS + 1, max: 1e9 }), (gap) => {
        const before = session.getSessionId();
        now += gap;
        clock.set(now);

        const rotated = session.getSessionId();
        expect(rotated.startsWith(`${now.toString(BASE36)}-`)).toBe(true);
        expect(rotated).toMatch(SESSION_ID_SHAPE);
        expect(rotated).not.toBe(before);
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

  it('rotates the id on sign-out cleanup but keeps it across reads without one', () => {
    const { session } = loadFreshSession(1000);
    const before = session.getSessionId();
    expect(session.getSessionId()).toBe(before);

    require('@shared/session/signOutCleanup').runSignOutCleanups();

    expect(session.getSessionId()).not.toBe(before);
  });
});

describe('eventSession module surface', () => {
  it('exposes only getSessionId, keeping the inactivity window and rotation private', () => {
    expect(Object.keys(SessionNamespace)).toEqual(['getSessionId']);
  });
});
