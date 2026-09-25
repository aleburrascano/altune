import { act, renderHook } from '@testing-library/react-native';

import { useSingleFlightAction } from '../useSingleFlightAction';

type Props = {
  open: boolean;
  resolve: () => Promise<string[]>;
  onResolveError?: (error: unknown) => void;
  onClose: () => void;
};

function setup(
  resolve: () => Promise<string[]>,
  open = true,
  onResolveError?: (error: unknown) => void,
) {
  const onClose = jest.fn();
  // exactOptionalPropertyTypes: the key is absent, never present-and-undefined.
  const initialProps: Props = onResolveError
    ? { open, resolve, onResolveError, onClose }
    : { open, resolve, onClose };
  const utils = renderHook((props: Props) => useSingleFlightAction(props), { initialProps });
  return { ...utils, onClose };
}

describe('useSingleFlightAction(): run', () => {
  it('hands a non-empty resolve to dispatch and releases the lock for the next gesture', async () => {
    const resolve = jest.fn(() => Promise.resolve(['a', 'b']));
    const dispatch = jest.fn();
    const { result } = setup(resolve);

    await act(() => result.current.run(dispatch));
    await act(() => result.current.run(dispatch));

    expect(resolve).toHaveBeenCalledTimes(2);
    expect(dispatch).toHaveBeenNthCalledWith(1, ['a', 'b']);
    expect(dispatch).toHaveBeenCalledTimes(2);
  });

  it('keeps the lock engaged after an empty resolve, dropping a replayed gesture', async () => {
    const resolve = jest.fn(() => Promise.resolve([] as string[]));
    const dispatch = jest.fn();
    const { result, onClose } = setup(resolve);

    await act(() => result.current.run(dispatch));
    await act(() => result.current.run(dispatch));

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(dispatch).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('drops a press replayed while the first resolve is still in flight, and reports resolving meanwhile', async () => {
    let settle: (items: string[]) => void = () => {};
    const resolve = jest.fn(() => new Promise<string[]>((r) => (settle = r)));
    const dispatch = jest.fn();
    const { result } = setup(resolve);

    let first: Promise<void> = Promise.resolve();
    act(() => {
      first = result.current.run(dispatch);
    });
    expect(result.current.resolving).toBe(true);
    await act(() => result.current.run(dispatch));
    expect(resolve).toHaveBeenCalledTimes(1);

    await act(async () => {
      settle(['x']);
      await first;
    });
    expect(result.current.resolving).toBe(false);
    expect(dispatch).toHaveBeenCalledTimes(1);
  });

  it('closes exactly once on a rejected resolve and drops the replay', async () => {
    const resolve = jest.fn(() => Promise.reject(new Error('save failed')));
    const dispatch = jest.fn();
    const { result, onClose } = setup(resolve);

    await act(() => result.current.run(dispatch));
    await act(() => result.current.run(dispatch));
    act(() => result.current.close());

    expect(resolve).toHaveBeenCalledTimes(1);
    expect(dispatch).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('hands the rejection to onResolveError before the close it forces', async () => {
    const failure = new Error('save failed');
    const order: string[] = [];
    const onResolveError = jest.fn(() => order.push('reported'));
    const { result, onClose } = setup(() => Promise.reject(failure), true, onResolveError);
    onClose.mockImplementation(() => order.push('closed'));

    await act(() => result.current.run(jest.fn()));

    expect(onResolveError).toHaveBeenCalledWith(failure);
    expect(order).toEqual(['reported', 'closed']);
  });

  it('clears both guards when the surface closes, so a fresh open can resolve and close again', async () => {
    const resolve = jest.fn(() => Promise.reject(new Error('save failed')));
    const { result, rerender, onClose } = setup(resolve);

    await act(() => result.current.run(jest.fn()));
    rerender({ open: false, resolve, onClose });
    rerender({ open: true, resolve, onClose });
    await act(() => result.current.run(jest.fn()));

    expect(resolve).toHaveBeenCalledTimes(2);
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

describe('useSingleFlightAction(): scheduled close', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('runs beforeClose then onClose once the delay elapses, and a later schedule replaces the earlier one', () => {
    const { result, onClose } = setup(() => Promise.resolve([]));
    const order: string[] = [];
    onClose.mockImplementation(() => order.push('close'));

    act(() => result.current.closeAfter(700, () => order.push('first')));
    act(() => jest.advanceTimersByTime(500));
    act(() => result.current.closeAfter(700, () => order.push('second')));
    act(() => jest.advanceTimersByTime(699));
    expect(onClose).not.toHaveBeenCalled();
    act(() => jest.advanceTimersByTime(1));

    expect(order).toEqual(['second', 'close']);
  });

  it('cancelScheduledClose prevents the pending close', () => {
    const { result, onClose } = setup(() => Promise.resolve([]));
    const beforeClose = jest.fn();

    act(() => result.current.closeAfter(700, beforeClose));
    act(() => result.current.cancelScheduledClose());
    act(() => jest.advanceTimersByTime(1000));

    expect(beforeClose).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
  });

  it('unmounting clears the pending close', () => {
    const { result, unmount, onClose } = setup(() => Promise.resolve([]));

    act(() => result.current.closeAfter(700, jest.fn()));
    unmount();
    jest.advanceTimersByTime(1000);

    expect(onClose).not.toHaveBeenCalled();
  });
});
