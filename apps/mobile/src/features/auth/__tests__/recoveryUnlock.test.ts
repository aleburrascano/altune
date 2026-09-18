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
