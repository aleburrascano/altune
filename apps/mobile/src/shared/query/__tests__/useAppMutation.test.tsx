import React from 'react';
import { isCancelledError, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/errors';
import { guardedMutationOptions, runSignOutCleanups } from '@shared/session/signOutCleanup';
import { reportClientError } from '@shared/telemetry/clientErrorReporting';
import { recordUserAction } from '@shared/telemetry/userTelemetry';

import { appMutationCache, appQueryCache, useAppMutation } from '../useAppMutation';
import { useOptimisticMutation } from '../useOptimisticMutation';

jest.mock('@shared/telemetry/userTelemetry', () => ({ recordUserAction: jest.fn() }));
jest.mock('@shared/telemetry/clientErrorReporting', () => ({ reportClientError: jest.fn() }));
jest.mock('@shared/ui/dialog/dialog', () => ({ showAlert: jest.fn() }));

const recordMock = recordUserAction as jest.Mock;
const reportMock = reportClientError as jest.Mock;

beforeEach(() => {
  recordMock.mockReset();
  reportMock.mockReset();
});

function newClient(): QueryClient {
  return new QueryClient({
    queryCache: appQueryCache(),
    mutationCache: appMutationCache(),
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function render<T>(queryClient: QueryClient, hook: () => T) {
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return renderHook(hook, { wrapper });
}

async function settle(run: () => Promise<unknown>): Promise<void> {
  await act(async () => {
    await run().catch(() => undefined);
  });
}

describe('useAppMutation()', () => {
  it('records one succeeded user_action with the track id, then runs the caller onSuccess', async () => {
    const onSuccess = jest.fn();
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.save',
        mutationFn: async (id: string) => id,
        trackIdOf: (id) => id,
        onSuccess,
      }),
    );
    await settle(() => result.current.mutateAsync('t1'));
    expect(recordMock).toHaveBeenCalledTimes(1);
    expect(recordMock).toHaveBeenCalledWith({
      action: 'demo.save',
      outcome: 'succeeded',
      track_id: 't1',
    });
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  it('records one failed user_action with status and correlation id, then runs the caller onError', async () => {
    const onError = jest.fn();
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.save',
        mutationFn: async (_: void) => {
          throw new ApiError(503, 'unavailable', 'x', 'corr-1');
        },
        onError,
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(recordMock).toHaveBeenCalledTimes(1);
    expect(recordMock).toHaveBeenCalledWith({
      action: 'demo.save',
      outcome: 'failed',
      status: 503,
      correlation_id: 'corr-1',
      error: 'unavailable',
    });
    expect(onError).toHaveBeenCalledTimes(1);
  });

  it('records a failed action without status for a non-API error', async () => {
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.save',
        mutationFn: async (_: void) => {
          throw new NetworkError('timeout', 'slow', 'corr-2');
        },
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(recordMock).toHaveBeenCalledWith({
      action: 'demo.save',
      outcome: 'failed',
      correlation_id: 'corr-2',
      error: 'slow',
    });
  });

  it('records nothing for a mutation fenced by sign-out', async () => {
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.fenced',
        ...guardedMutationOptions({
          mutationFn: async (_: void) => 'ok',
          onMutate: () => {
            runSignOutCleanups();
            return {};
          },
        }),
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(recordMock).not.toHaveBeenCalled();
  });
});

describe('appMutationCache() and appQueryCache()', () => {
  it('reports a failed mutation as source mutation', async () => {
    const boom = new Error('boom');
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.save',
        mutationFn: async (_: void) => {
          throw boom;
        },
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(reportMock).toHaveBeenCalledWith(boom, 'mutation');
  });

  it('reports a failed query as source query', async () => {
    const boom = new Error('nope');
    const queryClient = newClient();
    await queryClient
      .fetchQuery({
        queryKey: ['q'],
        queryFn: async () => {
          throw boom;
        },
      })
      .catch(() => undefined);
    expect(reportMock).toHaveBeenCalledWith(boom, 'query');
  });

  it('skips a cancelled query', async () => {
    const queryClient = newClient();
    const pending = queryClient
      .fetchQuery({
        queryKey: ['slow'],
        queryFn: ({ signal }) =>
          new Promise((_, reject) => {
            signal.addEventListener('abort', () => reject(new Error('aborted')));
          }),
      })
      .catch((error) => error);
    await queryClient.cancelQueries({ queryKey: ['slow'] });
    expect(isCancelledError(await pending)).toBe(true);
    expect(reportMock).not.toHaveBeenCalled();
  });

  it('skips a telemetry-gated failure', async () => {
    const gated = Object.assign(new Error('gated'), { name: 'TelemetryGatedError' });
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.gated',
        mutationFn: async (_: void) => {
          throw gated;
        },
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(reportMock).not.toHaveBeenCalled();
  });

  it('skips a SessionEndedError from guardedMutationOptions', async () => {
    const { result } = render(newClient(), () =>
      useAppMutation({
        action: 'demo.fenced',
        ...guardedMutationOptions({
          mutationFn: async (_: void) => 'ok',
          onMutate: () => {
            runSignOutCleanups();
            return {};
          },
        }),
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(reportMock).not.toHaveBeenCalled();
  });

  it('skips a SessionEndedError from useOptimisticMutation and records no user_action', async () => {
    const queryClient = newClient();
    queryClient.setQueryData(['n'], 1);
    const { result } = render(queryClient, () =>
      useOptimisticMutation({
        action: 'demo.optimistic',
        queryKey: ['n'],
        mutationFn: async (_: void) => {
          return 1;
        },
        applyOptimistic: (previous: number) => {
          runSignOutCleanups();
          return previous + 1;
        },
        revertOptimistic: (current: number) => current,
      }),
    );
    await settle(() => result.current.mutateAsync());
    expect(reportMock).not.toHaveBeenCalled();
    expect(recordMock).not.toHaveBeenCalled();
  });
});
