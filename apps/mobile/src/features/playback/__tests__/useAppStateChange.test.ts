import { renderHook } from '@testing-library/react-native';
import { AppState, type AppStateStatus } from 'react-native';

import { useAppStateChange } from '../hooks/useAppStateChange';

type Listener = (state: AppStateStatus) => void;

let listeners: Listener[];
let removes: jest.Mock[];

beforeEach(() => {
  listeners = [];
  removes = [];
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    listeners.push(listener as Listener);
    const remove = jest.fn(() => {
      listeners = listeners.filter((l) => l !== listener);
    });
    removes.push(remove);
    return { remove };
  });
});

afterEach(() => {
  jest.restoreAllMocks();
});

function emit(state: AppStateStatus): void {
  for (const l of [...listeners]) l(state);
}

describe('useAppStateChange', () => {
  it('forwards change events to the callback and removes the listener on unmount', () => {
    const onChange = jest.fn();
    const { unmount } = renderHook(() => useAppStateChange(onChange));

    expect(AppState.addEventListener).toHaveBeenCalledWith('change', onChange);
    emit('background');
    expect(onChange).toHaveBeenCalledWith('background');

    unmount();
    expect(removes[0]).toHaveBeenCalledTimes(1);
    emit('active');
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('swaps the subscription when the callback changes', () => {
    const first = jest.fn();
    const second = jest.fn();
    const { rerender } = renderHook(({ cb }: { cb: Listener }) => useAppStateChange(cb), {
      initialProps: { cb: first },
    });

    rerender({ cb: second });
    expect(removes[0]).toHaveBeenCalledTimes(1);
    emit('active');
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledWith('active');
    expect(listeners).toHaveLength(1);
  });
});
