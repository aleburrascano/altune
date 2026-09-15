import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { useOptimisticMutation } from '../useOptimisticMutation';

type Counter = { count: number };
const KEY = ['counter'] as const;
const OTHER_KEY = ['other'] as const;

let alertSpy: jest.SpyInstance;
beforeEach(() => {
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});
afterEach(() => {
  alertSpy.mockRestore();
});

function newClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const bump = (previous: Counter, by: number): Counter => ({ count: previous.count + by });
const failing = () => Promise.reject(new Error('boom'));

describe('useOptimisticMutation(): guarded (default)', () => {
  it('cancels the key, writes optimistically, and keeps the write on success', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    const cancelSpy = jest.spyOn(queryClient, 'cancelQueries');
    let seenMidFlight: Counter | undefined;

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: async (by: number) => {
            seenMidFlight = queryClient.getQueryData<Counter>(KEY);
            return by;
          },
          applyOptimistic: bump,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await result.current.mutateAsync(2);
    });

    expect(cancelSpy).toHaveBeenCalledWith({ queryKey: KEY });
    expect(seenMidFlight).toEqual({ count: 3 });
    expect(queryClient.getQueryData(KEY)).toEqual({ count: 3 });
  });

  it('leaves a cold cache untouched and skips the rollback on failure', async () => {
    const queryClient = newClient();
    const setSpy = jest.spyOn(queryClient, 'setQueryData');
    const applyOptimistic = jest.fn(bump);

    const { result } = renderHook(
      () => useOptimisticMutation({ queryKey: KEY, mutationFn: failing, applyOptimistic }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(2)).rejects.toThrow('boom');
    });

    expect(applyOptimistic).not.toHaveBeenCalled();
    expect(setSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(KEY)).toBeUndefined();
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('rolls back to the snapshot and shows the alert built from the variables', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: failing,
          applyOptimistic: bump,
          alertOnError: (by: number) => ({ title: 'Failed', message: `Could not add ${by}.` }),
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(4)).rejects.toThrow('boom');
    });

    expect(queryClient.getQueryData(KEY)).toEqual({ count: 1 });
    expect(alertSpy).toHaveBeenCalledWith('Failed', 'Could not add 4.');
  });

  it('invalidates every key from `invalidate` and settles only after they finish', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    let releaseInvalidation!: () => void;
    const invalidateSpy = jest
      .spyOn(queryClient, 'invalidateQueries')
      .mockImplementation(() => new Promise<void>((resolve) => (releaseInvalidation = resolve)));
    const onSuccess = jest.fn();

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: async (by: number) => by * 10,
          applyOptimistic: bump,
          onSuccess,
          invalidate: () => [OTHER_KEY],
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    let settled = false;
    await act(async () => {
      void result.current.mutateAsync(1).then(() => (settled = true));
      for (let i = 0; i < 10; i += 1) await Promise.resolve();
    });

    expect(onSuccess).toHaveBeenCalledWith(10, 1, expect.anything(), expect.anything());
    expect(invalidateSpy).toHaveBeenCalledTimes(1);
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: OTHER_KEY });
    expect(settled).toBe(false);

    await act(async () => {
      releaseInvalidation();
      for (let i = 0; i < 10; i += 1) await Promise.resolve();
    });
    expect(settled).toBe(true);
  });

  it('invalidates the optimistic key itself when no `invalidate` is given', async () => {
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: async (by: number) => by,
          applyOptimistic: bump,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await result.current.mutateAsync(1);
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: KEY });
  });
});

describe('useOptimisticMutation(): unguarded', () => {
  it('does not cancel, writes against a cold cache, and rolls back unconditionally without alerting', async () => {
    const queryClient = newClient();
    const cancelSpy = jest.spyOn(queryClient, 'cancelQueries');
    let seenMidFlight: Counter | undefined;

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          unguarded: true,
          mutationFn: (by: number) => {
            seenMidFlight = queryClient.getQueryData<Counter>(KEY);
            return Promise.reject(new Error(`boom ${by}`));
          },
          applyOptimistic: (previous: Counter | undefined, by) => ({
            count: (previous?.count ?? 0) + by,
          }),
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(5)).rejects.toThrow('boom 5');
    });

    expect(cancelSpy).not.toHaveBeenCalled();
    expect(seenMidFlight).toEqual({ count: 5 });
    expect(queryClient.getQueryData(KEY)).toEqual({ count: 5 });
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('restores a warm snapshot on failure and does not wait for invalidation to settle', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    jest
      .spyOn(queryClient, 'invalidateQueries')
      .mockImplementation(() => new Promise<void>(() => {}));

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          unguarded: true,
          mutationFn: failing,
          applyOptimistic: (previous: Counter | undefined, by: number) => ({
            count: (previous?.count ?? 0) + by,
          }),
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(3)).rejects.toThrow('boom');
    });

    expect(queryClient.getQueryData(KEY)).toEqual({ count: 1 });
  });
});
