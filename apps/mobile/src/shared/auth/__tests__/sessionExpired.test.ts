import { act, renderHook } from '@testing-library/react-native';

import type * as SessionExpiredModule from '../sessionExpired';
import {
  clearSessionExpired,
  getSessionExpired,
  markSessionExpired,
  useSessionExpired,
} from '../sessionExpired';

let activeUnmounts: (() => void)[] = [];

function mountTrackedHook() {
  let renders = 0;
  const { result, unmount } = renderHook(() => {
    renders += 1;
    return useSessionExpired();
  });
  activeUnmounts.push(unmount);
  return { result, getRenderCount: () => renders };
}

beforeEach(() => {
  act(() => {
    clearSessionExpired();
  });
});

afterEach(() => {
  activeUnmounts.forEach((unmount) => unmount());
  activeUnmounts = [];
  act(() => {
    clearSessionExpired();
  });
});

describe('module initial state', () => {
  it('a freshly launched app is not in the session-expired state', () => {
    let freshModule!: typeof SessionExpiredModule;

    jest.isolateModules(() => {
      freshModule = require('../sessionExpired');
    });

    expect(freshModule.getSessionExpired()).toBe(false);
  });
});

describe('markSessionExpired / clearSessionExpired — table', () => {
  it('flips false -> true and notifies a subscriber when the session was not yet expired', () => {
    const { result, getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      markSessionExpired();
    });

    expect(getSessionExpired()).toBe(true);
    expect(result.current).toBe(true);
    expect(getRenderCount()).toBeGreaterThan(before);
  });

  it('is a no-op that leaves expired true and produces no re-render when already expired', () => {
    act(() => {
      markSessionExpired();
    });
    const { getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      markSessionExpired();
    });

    expect(getSessionExpired()).toBe(true);
    expect(getRenderCount()).toBe(before);
  });

  it('flips true -> false and notifies a subscriber when the session was expired', () => {
    act(() => {
      markSessionExpired();
    });
    const { result, getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      clearSessionExpired();
    });

    expect(getSessionExpired()).toBe(false);
    expect(result.current).toBe(false);
    expect(getRenderCount()).toBeGreaterThan(before);
  });

  it('is a no-op that leaves expired false and produces no re-render when already cleared', () => {
    const { getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      clearSessionExpired();
    });

    expect(getSessionExpired()).toBe(false);
    expect(getRenderCount()).toBe(before);
  });
});

describe('state x subscribe — a hook mounted mid-flight reads the current state, not a stale default', () => {
  it('a hook mounted while not expired reads false immediately', () => {
    const { result } = mountTrackedHook();

    expect(result.current).toBe(false);
  });

  it('a hook mounted while already expired reads true immediately, with no mark event of its own', () => {
    act(() => {
      markSessionExpired();
    });

    const { result } = mountTrackedHook();

    expect(result.current).toBe(true);
  });
});

describe('idempotence / replay — a burst of concurrent 401s all mark the same session', () => {
  it('two marks delivered in sequence to the same subscriber produce exactly one notification, not two', () => {
    const { getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      markSessionExpired();
    });
    act(() => {
      markSessionExpired();
    });

    expect(getSessionExpired()).toBe(true);
    expect(getRenderCount()).toBe(before + 1);
  });

  it('two clears delivered in sequence to the same subscriber produce exactly one notification, not two', () => {
    act(() => {
      markSessionExpired();
    });
    const { getRenderCount } = mountTrackedHook();
    const before = getRenderCount();

    act(() => {
      clearSessionExpired();
    });
    act(() => {
      clearSessionExpired();
    });

    expect(getSessionExpired()).toBe(false);
    expect(getRenderCount()).toBe(before + 1);
  });
});
