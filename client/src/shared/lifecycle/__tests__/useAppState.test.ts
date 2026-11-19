import { act, renderHook } from '@testing-library/react-native';

import { useAppStateChange, useIsForeground, type AppLifecycleState } from '@shared/lifecycle';
import {
  driveAppState,
  type AppStateDriver,
} from '@testing/fakes/lifecycle/inMemoryAppStateSource';

type Listener = (state: AppLifecycleState) => void;

let app: AppStateDriver;

beforeEach(() => {
  app = driveAppState();
});

describe('useAppStateChange', () => {
  it('forwards change events to the callback and stops once unmounted', () => {
    const onChange = jest.fn();
    const { unmount } = renderHook(() => useAppStateChange(onChange));

    app.change('background');
    expect(onChange).toHaveBeenCalledWith('background');

    unmount();
    app.change('active');
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('runs only the latest callback, once per change, after a rerender', () => {
    const first = jest.fn();
    const second = jest.fn();
    const { rerender } = renderHook(({ cb }: { cb: Listener }) => useAppStateChange(cb), {
      initialProps: { cb: first },
    });

    rerender({ cb: second });
    app.change('active');

    expect(first).not.toHaveBeenCalled();
    expect(second.mock.calls).toEqual([['active']]);
  });
});

describe('useIsForeground', () => {
  it('starts from whether the app is active', () => {
    driveAppState('background');
    const { result } = renderHook(() => useIsForeground());
    expect(result.current).toBe(false);
  });

  it('follows the app into the background and back', () => {
    const { result } = renderHook(() => useIsForeground());
    expect(result.current).toBe(true);

    act(() => app.change('background'));
    expect(result.current).toBe(false);

    act(() => app.change('active'));
    expect(result.current).toBe(true);
  });
});
