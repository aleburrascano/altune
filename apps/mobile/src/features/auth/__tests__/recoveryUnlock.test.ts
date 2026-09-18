import { act, renderHook } from '@testing-library/react-native';

import {
  clearRecoveryUnlock,
  isRecoveryUnlocked,
  markRecoveryUnlocked,
  RECOVERY_UNLOCK_WINDOW_MS,
  useRecoveryUnlocked,
  _listenerCountForTest,
} from '../recoveryUnlock';

const VERIFIED_USER = 'user-a';
const OTHER_USER = 'user-b';

beforeEach(() => {
  clearRecoveryUnlock();
});

afterEach(() => {
  clearRecoveryUnlock();
});

describe('recoveryUnlock marker', () => {
  it('is locked by default', () => {
    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });

  it('unlocks for the window after markRecoveryUnlocked', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(true);
    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS - 1)).toBe(true);
  });

  it('re-locks once the window has elapsed', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS)).toBe(false);
    expect(isRecoveryUnlocked(VERIFIED_USER, now + RECOVERY_UNLOCK_WINDOW_MS + 1)).toBe(false);
  });

  it('clearRecoveryUnlock re-locks immediately', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);
    clearRecoveryUnlock();

    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(false);
  });

  it('notifies subscribers on mark and clear, and detaches on unmount', () => {
    let renders = 0;
    const { result, unmount } = renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });

    expect(result.current).toBe(false);
    const before = renders;

    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);
    expect(renders).toBeGreaterThan(before);

    act(() => clearRecoveryUnlock());
    expect(result.current).toBe(false);

    expect(_listenerCountForTest()).toBe(1);
    unmount();
    expect(_listenerCountForTest()).toBe(0);
  });

  it('clearRecoveryUnlock when already locked emits nothing extra', () => {
    let renders = 0;
    const { unmount } = renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });
    const before = renders;

    act(() => clearRecoveryUnlock());

    expect(renders).toBe(before);
    unmount();
  });
});

// Issue #1638: the window used to be a bare deadline with no owner, so an
// abandoned recovery unlocked the reset form for whatever account was active
// next on the same process.
describe('recoveryUnlock is bound to the identity the recovery was verified for', () => {
  it('stays locked for another account well inside the window', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(OTHER_USER, now)).toBe(false);
    expect(isRecoveryUnlocked(OTHER_USER, now + RECOVERY_UNLOCK_WINDOW_MS - 1)).toBe(false);
  });

  it('stays locked when nobody is signed in to compare against', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    expect(isRecoveryUnlocked(null, now)).toBe(false);
  });

  it('hands the window to the second account when a second recovery is verified, and takes it from the first', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(VERIFIED_USER, now);

    markRecoveryUnlocked(OTHER_USER, now);

    expect(isRecoveryUnlocked(OTHER_USER, now)).toBe(true);
    expect(isRecoveryUnlocked(VERIFIED_USER, now)).toBe(false);
  });

  it('re-renders a subscriber locked once the window it is watching belongs to someone else', () => {
    const { result, rerender } = renderHook(
      ({ userId }: { userId: string }) => useRecoveryUnlocked(userId),
      { initialProps: { userId: VERIFIED_USER } },
    );
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);

    rerender({ userId: OTHER_USER });

    expect(result.current).toBe(false);
  });
});

// Issue #1639: the cutoff is what keeps an abandoned marker from being spent
// minutes later, so it has to hold against a device clock its owner can set, and
// it has to land on a screen already mounted rather than on the next render.
describe('recoveryUnlock is bounded by elapsed time no device clock can fake', () => {
  const MARKED_AT = 1_000_000;
  const MARKED_TICK = 5_000;
  const A_MONTH_MS = 30 * 24 * 60 * 60 * 1000;

  afterEach(() => {
    act(() => clearRecoveryUnlock());
    jest.useRealTimers();
  });

  it('stays locked when the wall clock is rolled back behind the moment it was marked', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(isRecoveryUnlocked(VERIFIED_USER, MARKED_AT - 1, MARKED_TICK)).toBe(false);
    expect(isRecoveryUnlocked(VERIFIED_USER, MARKED_AT - A_MONTH_MS, MARKED_TICK)).toBe(false);
  });

  it('stays locked once the window of real elapsed time is spent, with the wall clock held still', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(
      isRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK + RECOVERY_UNLOCK_WINDOW_MS),
    ).toBe(false);
  });

  it('stays unlocked while both clocks agree the window is still open', () => {
    markRecoveryUnlocked(VERIFIED_USER, MARKED_AT, MARKED_TICK);

    expect(
      isRecoveryUnlocked(
        VERIFIED_USER,
        MARKED_AT + RECOVERY_UNLOCK_WINDOW_MS - 1,
        MARKED_TICK + RECOVERY_UNLOCK_WINDOW_MS - 1,
      ),
    ).toBe(true);
  });

  // setSystemTime moves Date.now() and leaves performance.now() where it is,
  // which is precisely what setting the device clock back does to the defaults.
  it('stays locked after a rollback against the clocks it reads by default', () => {
    jest.useFakeTimers();
    jest.setSystemTime(MARKED_AT + A_MONTH_MS);
    markRecoveryUnlocked(VERIFIED_USER);

    jest.setSystemTime(MARKED_AT);

    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });
});

describe('recoveryUnlock demotes a mounted subscriber the moment the window elapses', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    act(() => clearRecoveryUnlock());
    jest.useRealTimers();
  });

  it('holds a subscriber unlocked right up to the cutoff', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));

    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS - 1);
    });

    expect(result.current).toBe(true);
  });

  it('locks a subscriber that never re-rendered once the cutoff passes', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);

    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });

    expect(result.current).toBe(false);
  });

  // The real ordering: completeAuthIntent opens the window, and AuthGate mounts
  // onto the reset-password route afterwards.
  it('demotes a subscriber that mounted onto a window already open', () => {
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS / 2);
    });

    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    expect(result.current).toBe(true);
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS / 2);
    });

    expect(result.current).toBe(false);
  });

  it('gives a second verified recovery a full window of its own', () => {
    const { result } = renderHook(() => useRecoveryUnlocked(VERIFIED_USER));
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });

    act(() => markRecoveryUnlocked(VERIFIED_USER));

    expect(result.current).toBe(true);
    act(() => {
      jest.advanceTimersByTime(RECOVERY_UNLOCK_WINDOW_MS);
    });
    expect(result.current).toBe(false);
  });

  it('stops emitting once the window was closed by hand, leaving no timer behind', () => {
    let renders = 0;
    renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked(VERIFIED_USER);
    });
    act(() => markRecoveryUnlocked(VERIFIED_USER));
    act(() => clearRecoveryUnlock());
    const before = renders;

    act(() => {
      jest.advanceTimersByTime(2 * RECOVERY_UNLOCK_WINDOW_MS);
    });

    expect(renders).toBe(before);
  });
});
