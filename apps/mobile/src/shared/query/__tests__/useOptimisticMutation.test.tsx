import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';

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
const unbump = (current: Counter, by: number): Counter => bump(current, -by);
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
          revertOptimistic: unbump,
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
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: failing,
          applyOptimistic,
          revertOptimistic: unbump,
        }),
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

  it('reverts the optimistic write and shows the alert built from the variables', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: failing,
          applyOptimistic: bump,
          revertOptimistic: unbump,
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

  it('rolls back by reverting its own delta on the current cache, keeping a write that landed mid-flight', async () => {
    const queryClient = newClient();
    queryClient.setQueryData<Counter & { label: string }>(KEY, { count: 1, label: 'old' });
    const revertOptimistic = jest.fn((current: Counter & { label: string }, by: number) => ({
      ...current,
      count: current.count - by,
    }));

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: () => {
            queryClient.setQueryData<Counter & { label: string }>(KEY, (prev) => ({
              ...prev!,
              label: 'from sse',
            }));
            return failing();
          },
          applyOptimistic: (previous: Counter & { label: string }, by: number) => ({
            ...previous,
            count: previous.count + by,
          }),
          revertOptimistic,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(4)).rejects.toThrow('boom');
    });

    expect(revertOptimistic).toHaveBeenCalledWith({ count: 5, label: 'from sse' }, 4, {
      count: 1,
      label: 'old',
    });
    expect(queryClient.getQueryData(KEY)).toEqual({ count: 1, label: 'from sse' });
  });

  it('skips the rollback when the cache was cleared mid-flight', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    const revertOptimistic = jest.fn(unbump);

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: (_by: number) => {
            queryClient.removeQueries({ queryKey: KEY });
            return failing();
          },
          applyOptimistic: bump,
          revertOptimistic,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(2)).rejects.toThrow('boom');
    });

    expect(revertOptimistic).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(KEY)).toBeUndefined();
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
          revertOptimistic: unbump,
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
          revertOptimistic: unbump,
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

/** Sign-out plus another sign-in: the epoch moves on and the key now holds the new user's data. */
function switchUser(queryClient: QueryClient, theirCache: Counter): void {
  runSignOutCleanups();
  queryClient.setQueryData(KEY, theirCache);
}

describe('useOptimisticMutation(): session fencing', () => {
  it('leaves the next user data in place when an unguarded rollback lands after a user switch', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          unguarded: true,
          mutationFn: (_by: number) => {
            switchUser(queryClient, { count: 99 });
            return failing();
          },
          applyOptimistic: (previous: Counter | undefined, by: number) => ({
            count: (previous?.count ?? 0) + by,
          }),
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(3)).rejects.toThrow('boom');
    });

    expect(queryClient.getQueryData(KEY)).toEqual({ count: 99 });
  });

  it('does not revert its own delta against the next user cache after a user switch', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    const revertOptimistic = jest.fn(unbump);

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: (_by: number) => {
            switchUser(queryClient, { count: 99 });
            return failing();
          },
          applyOptimistic: bump,
          revertOptimistic,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await expect(result.current.mutateAsync(4)).rejects.toThrow('boom');
    });

    expect(revertOptimistic).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(KEY)).toEqual({ count: 99 });
  });

  it('does not invalidate the next user queries when the mutation settles after a user switch', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(KEY, { count: 1 });
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(
      () =>
        useOptimisticMutation({
          queryKey: KEY,
          mutationFn: async (by: number) => {
            switchUser(queryClient, { count: 99 });
            return by;
          },
          applyOptimistic: bump,
          revertOptimistic: unbump,
        }),
      { wrapper: wrapperFor(queryClient) },
    );

    await act(async () => {
      await result.current.mutateAsync(2);
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(KEY)).toEqual({ count: 99 });
  });
});

describe('session fencing of the request and its callbacks', () => {
  type Counter = { count: number };
  const USER_B_COUNTER: Counter = { count: 99 };

  function newClient(): QueryClient {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    queryClient.setQueryData(KEY, { count: 1 });
    return queryClient;
  }

  function signOutThenLoadUserB(queryClient: QueryClient): void {
    runSignOutCleanups();
    queryClient.clear();
    queryClient.setQueryData(KEY, USER_B_COUNTER);
  }

  let alertSpy: jest.SpyInstance;
  beforeEach(() => {
    alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  });
  afterEach(() => {
    alertSpy.mockRestore();
  });

  // Regression test for #2729.
  describe('useOptimisticMutation(): fencing the request and its callbacks by session', () => {
    it('shows no error alert to the next user when the request fails after a sign-out', async () => {
      const queryClient = newClient();
      const { result } = renderHook(
        () =>
          useOptimisticMutation({
            queryKey: KEY,
            mutationFn: (_by: number) => {
              signOutThenLoadUserB(queryClient);
              return Promise.reject(new Error('boom'));
            },
            applyOptimistic: bump,
            revertOptimistic: unbump,
            alertOnError: () => ({ title: 'Failed', message: 'try again' }),
          }),
        { wrapper: wrapperFor(queryClient) },
      );

      await act(async () => {
        await expect(result.current.mutateAsync(2)).rejects.toThrow('boom');
      });

      expect(alertSpy).not.toHaveBeenCalled();
      expect(queryClient.getQueryData(KEY)).toEqual(USER_B_COUNTER);
    });

    it("does not run the caller's onSuccess when the request succeeds after a sign-out", async () => {
      const queryClient = newClient();
      const onSuccess = jest.fn();
      const { result } = renderHook(
        () =>
          useOptimisticMutation({
            queryKey: KEY,
            mutationFn: async (by: number) => {
              signOutThenLoadUserB(queryClient);
              return by;
            },
            applyOptimistic: bump,
            revertOptimistic: unbump,
            onSuccess,
          }),
        { wrapper: wrapperFor(queryClient) },
      );

      await act(async () => {
        await result.current.mutateAsync(2);
      });

      expect(onSuccess).not.toHaveBeenCalled();
    });

    it('never sends the request, nor writes optimistically, when the user signs out while in-flight fetches are cancelled', async () => {
      const queryClient = newClient();
      const mutationFn = jest.fn(async (by: number) => by);
      jest.spyOn(queryClient, 'cancelQueries').mockImplementation(async () => {
        signOutThenLoadUserB(queryClient);
      });
      const { result } = renderHook(
        () =>
          useOptimisticMutation({
            queryKey: KEY,
            mutationFn,
            applyOptimistic: bump,
            revertOptimistic: unbump,
          }),
        { wrapper: wrapperFor(queryClient) },
      );

      await act(async () => {
        await expect(result.current.mutateAsync(2)).rejects.toThrow();
      });

      expect(mutationFn).not.toHaveBeenCalled();
      expect(queryClient.getQueryData(KEY)).toEqual(USER_B_COUNTER);
    });
  });
});
