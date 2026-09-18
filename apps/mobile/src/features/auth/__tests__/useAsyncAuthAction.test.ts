import { renderHook, act } from '@testing-library/react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { useAsyncAuthAction } from '../hooks/useAsyncAuthAction';

type Result =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

type Outcome = Exclude<Result, { kind: 'idle' } | { kind: 'pending' }>;

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

/** An SDK call the test decides the settle time of, counting each invocation. */
function deferredAttempt(): {
  attempt: jest.Mock<Promise<Outcome>, []>;
  settleWith: (outcome: Outcome) => void;
} {
  let settle!: (outcome: Outcome) => void;
  const call = new Promise<Outcome>((resolve) => {
    settle = resolve;
  });
  return { attempt: jest.fn(() => call), settleWith: (outcome) => settle(outcome) };
}

describe('useAsyncAuthAction: rejecting a duplicate submit at the hook (#1643)', () => {
  // An unrecognized failure is logged now (#1647), and the thrown fixture below
  // is deliberate: silenced rather than printed through the suite's output.
  beforeEach(() => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('reaches the SDK once when a second press lands before the first settles', async () => {
    const { attempt, settleWith } = deferredAttempt();
    const { result } = renderHook(() => useAsyncAuthAction<Result, []>(attempt));

    let presses!: Promise<unknown>;
    act(() => {
      // Both land in the same tick, so neither has seen the `pending` render
      // that the caller's `disabled` button relies on.
      presses = Promise.all([result.current.run(), result.current.run()]);
    });

    expect(attempt).toHaveBeenCalledTimes(1);

    await act(async () => {
      settleWith({ kind: 'ok' });
      await presses;
    });
    expect(result.current.state).toEqual({ kind: 'ok' });
  });

  it('lets the next submit through once the first has settled', async () => {
    const attempt = jest.fn(async () => ({ kind: 'ok' }) as const);
    const { result } = renderHook(() => useAsyncAuthAction<Result, []>(attempt));

    await act(async () => {
      await result.current.run();
    });
    await act(async () => {
      await result.current.run();
    });

    expect(attempt).toHaveBeenCalledTimes(2);
  });

  it('lets the user retry after an attempt that threw', async () => {
    const attempt = jest
      .fn<Promise<Outcome>, []>()
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValueOnce({ kind: 'ok' });
    const { result } = renderHook(() => useAsyncAuthAction<Result, []>(attempt));

    await act(async () => {
      await result.current.run();
    });
    expect(result.current.state).toEqual({ kind: 'error', reason: 'unknown' });

    await act(async () => {
      await result.current.run();
    });
    expect(result.current.state).toEqual({ kind: 'ok' });
  });
});

const UNRECOGNIZED_FAILURE_LOG = '[auth] an auth action failed for an unrecognized reason';

/** The terminal state of a hook whose one SDK call rejected with `thrown`. */
async function stateAfterRejectionWith(thrown: unknown): Promise<Result> {
  const attempt = jest.fn<Promise<Outcome>, []>().mockRejectedValue(thrown);
  const { result } = renderHook(() => useAsyncAuthAction<Result, []>(attempt));

  await act(async () => {
    await result.current.run();
  });
  return result.current.state;
}

describe('useAsyncAuthAction: what an unknown failure leaves behind (#1647)', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it("logs the thrown error's name and message, which the unknown reason alone drops", async () => {
    const rejected = Object.assign(new Error('Database error granting user'), {
      name: 'AuthApiError',
    });

    const state = await stateAfterRejectionWith(rejected);

    expect(state).toEqual({ kind: 'error', reason: 'unknown' });
    expect(warn).toHaveBeenCalledWith(UNRECOGNIZED_FAILURE_LOG, {
      name: 'AuthApiError',
      message: 'Database error granting user',
    });
  });

  it('logs nothing for a failure the network reason already names', async () => {
    const state = await stateAfterRejectionWith(new Error('Network request failed'));

    expect(state).toEqual({ kind: 'error', reason: 'network' });
    expect(warn).not.toHaveBeenCalled();
  });

  it('names the type of a thrown non-Error rather than spelling the value out', async () => {
    // Whatever a rejected SDK call threw is beyond this hook's knowledge, and
    // stringifying it could spell out the request that carried the credential.
    const state = await stateAfterRejectionWith('token_hash=super-secret-hash');

    expect(state).toEqual({ kind: 'error', reason: 'unknown' });
    expect(warn).toHaveBeenCalledWith(UNRECOGNIZED_FAILURE_LOG, {
      name: 'string',
      message: 'a non-Error value was thrown',
    });
  });
});
