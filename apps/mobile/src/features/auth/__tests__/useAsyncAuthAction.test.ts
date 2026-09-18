import { renderHook, act } from '@testing-library/react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { useAsyncAuthAction } from '../hooks/useAsyncAuthAction';

type Result =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

describe('useAsyncAuthAction: the deadline bounding a stalled SDK call', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('reaches a terminal network error when the Supabase call never resolves', async () => {
    const neverSettles = (): Promise<{ kind: 'ok' }> => new Promise<{ kind: 'ok' }>(() => {});
    const { result } = renderHook(() => useAsyncAuthAction<Result, []>(neverSettles));

    let runCall!: Promise<void>;
    act(() => {
      runCall = result.current.run();
    });

    // The button is pinned at pending until the deadline decides otherwise.
    expect(result.current.state).toEqual({ kind: 'pending' });

    // One tick before the budget it is still pending — nothing has abandoned it.
    act(() => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS - 1);
    });
    expect(result.current.state).toEqual({ kind: 'pending' });

    // At the budget the call is abandoned and mapped to a terminal error.
    await act(async () => {
      jest.advanceTimersByTime(1);
      await runCall;
    });
    expect(result.current.state).toEqual({ kind: 'error', reason: 'network' });
  });

  it('lets a call that settles before the deadline reach its own terminal state', async () => {
    const settlesOk = (): Promise<{ kind: 'ok' }> => Promise.resolve({ kind: 'ok' });
    const { result } = renderHook(() => useAsyncAuthAction<Result, []>(settlesOk));

    await act(async () => {
      await result.current.run();
    });

    expect(result.current.state).toEqual({ kind: 'ok' });

    // The timer is released, so advancing past the budget can't overwrite the result.
    act(() => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS);
    });
    expect(result.current.state).toEqual({ kind: 'ok' });
  });
});
