import { act, renderHook } from '@testing-library/react-native';

import {
  clearRecoveryUnlock,
  isRecoveryUnlocked,
  markRecoveryUnlocked,
  RECOVERY_UNLOCK_WINDOW_MS,
  useRecoveryUnlocked,
  _listenerCountForTest,
} from '../lib/recoveryUnlock';

beforeEach(() => {
  clearRecoveryUnlock();
});

afterEach(() => {
  clearRecoveryUnlock();
});

describe('recoveryUnlock marker', () => {
  it('is locked by default', () => {
    expect(isRecoveryUnlocked()).toBe(false);
  });

  it('unlocks for the window after markRecoveryUnlocked', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(now);

    expect(isRecoveryUnlocked(now)).toBe(true);
    expect(isRecoveryUnlocked(now + RECOVERY_UNLOCK_WINDOW_MS - 1)).toBe(true);
  });

  it('re-locks once the window has elapsed', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(now);

    expect(isRecoveryUnlocked(now + RECOVERY_UNLOCK_WINDOW_MS)).toBe(false);
    expect(isRecoveryUnlocked(now + RECOVERY_UNLOCK_WINDOW_MS + 1)).toBe(false);
  });

  it('clearRecoveryUnlock re-locks immediately', () => {
    const now = 1_000_000;
    markRecoveryUnlocked(now);
    clearRecoveryUnlock();

    expect(isRecoveryUnlocked(now)).toBe(false);
  });

  it('notifies subscribers on mark and clear, and detaches on unmount', () => {
    let renders = 0;
    const { result, unmount } = renderHook(() => {
      renders += 1;
      return useRecoveryUnlocked();
    });

    expect(result.current).toBe(false);
    const before = renders;

    act(() => markRecoveryUnlocked());
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
      return useRecoveryUnlocked();
    });
    const before = renders;

    act(() => clearRecoveryUnlock());

    expect(renders).toBe(before);
    unmount();
  });
});
