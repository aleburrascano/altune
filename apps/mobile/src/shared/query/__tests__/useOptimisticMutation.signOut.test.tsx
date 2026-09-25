import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { useOptimisticMutation } from '../useOptimisticMutation';

type Counter = { count: number };
const KEY = ['counter'] as const;
const USER_B_COUNTER: Counter = { count: 99 };

const bump = (previous: Counter, by: number): Counter => ({ count: previous.count + by });
const unbump = (current: Counter, by: number): Counter => bump(current, -by);

function newClient(): QueryClient {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  queryClient.setQueryData(KEY, { count: 1 });
  return queryClient;
}

function wrapperFor(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
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

describe('useOptimisticMutation(): fencing the request and its callbacks by session (#2729)', () => {
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
