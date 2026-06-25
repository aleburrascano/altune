/**
 * useServerEvents — the event-type → React Query invalidation contract.
 *
 * The SSEClient is mocked to capture the onEvent handler the hook installs, so
 * the test can fire a server event and assert the right caches are invalidated.
 * This is the seam that keeps the UI (incl. the detail save-control lifecycle)
 * in sync when acquisition completes.
 */

import { renderHook } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import type { ReactNode } from 'react';

type ServerEvent = { id: string; type: string; data: Record<string, unknown> };
const captured: { onEvent?: ((event: ServerEvent) => void) | undefined } = {};

jest.mock('../sse-client', () => ({
  SSEClient: class {
    connect = jest.fn(async () => {});
    disconnect = jest.fn();
    dispose = jest.fn();
    constructor(
      _url: string,
      _getToken: unknown,
      onEvent: (event: ServerEvent) => void,
    ) {
      captured.onEvent = onEvent;
    }
  },
}));

jest.mock('../../auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn(async () => ({ data: { session: null } })) } },
}));

import { useServerEvents } from '../useServerEvents';

function makeWrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }): ReactNode =>
    createElement(QueryClientProvider, { client: qc }, children);
}

function setup(): jest.SpyInstance {
  const qc = new QueryClient();
  const invalidate = jest
    .spyOn(qc, 'invalidateQueries')
    .mockResolvedValue(undefined as never);
  renderHook(() => useServerEvents(), { wrapper: makeWrapper(qc) });
  return invalidate;
}

afterEach(() => {
  captured.onEvent = undefined;
});

describe('useServerEvents', () => {
  it('invalidates the library caches when acquisition completes', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '1', type: 'track_acquisition_completed', data: { track_id: 't1' } });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['library-home'] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['library'] });
  });

  it('invalidates the library caches when acquisition fails', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '2', type: 'track_acquisition_failed', data: { track_id: 't2' } });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['library'] });
  });

  it('ignores event types that are not in the invalidation map', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '3', type: 'something_unmapped', data: {} });
    expect(invalidate).not.toHaveBeenCalled();
  });
});
