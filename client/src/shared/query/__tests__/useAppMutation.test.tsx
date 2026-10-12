import React from 'react';
import { isCancelledError, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { ApiError, NetworkError } from '@shared/failure/errors';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { guardedMutationOptions } from '@shared/session/guardedMutation';
import { signInTestUser } from '@testing/signInTestUser';

import {
  appMutationCache,
  appQueryCache,
  useAppMutation,
  useOptimisticMutation,
} from '@shared/query/useAppMutation';

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS_PATH = '/v1/discovery/events';

type SentRequest = { path: string; body: string };
type SentEvent = { type: string; payload: Record<string, unknown> };

beforeEach(async () => {
  await signInTestUser();
  __http.reply(`POST ${EVENTS_PATH}`, { status: 202 });
});

async function sentEvents(type: string): Promise<SentEvent[]> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return (__http.requests as SentRequest[])
    .filter((request) => request.path === EVENTS_PATH)
    .map((request) => JSON.parse(request.body) as SentEvent)
    .filter((event) => event.type === type);
}

async function userActions(): Promise<Record<string, unknown>[]> {
  const events = await sentEvents('user_action');
  return events.map(({ payload: { session_id: _session, ...rest } }) => rest);
}

type ClientErrorSummary = { source: unknown; message: unknown };

async function clientErrors(): Promise<ClientErrorSummary[]> {
  const events = await sentEvents('client_error');
  return events.map(({ payload }) => ({ source: payload.source, message: payload.message }));
}

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
    expect(await userActions()).toEqual([
      { action: 'demo.save', outcome: 'succeeded', track_id: 't1' },
    ]);
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
    expect(await userActions()).toEqual([
      {
        action: 'demo.save',
        outcome: 'failed',
        status: 503,
        correlation_id: 'corr-1',
        error: 'unavailable',
      },
    ]);
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
    expect(await userActions()).toEqual([
      { action: 'demo.save', outcome: 'failed', correlation_id: 'corr-2', error: 'slow' },
    ]);
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
    expect(await userActions()).toEqual([]);
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
    expect(await clientErrors()).toEqual([{ source: 'mutation', message: 'boom' }]);
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
    expect(await clientErrors()).toEqual([{ source: 'query', message: 'nope' }]);
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
    expect(await clientErrors()).toEqual([]);
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
    expect(await clientErrors()).toEqual([]);
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
    expect(await clientErrors()).toEqual([]);
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
    expect(await clientErrors()).toEqual([]);
    expect(await userActions()).toEqual([]);
  });
});
