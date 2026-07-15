/**
 * useServerEvents — the event-type → React Query effect contract.
 *
 * The SSEClient is mocked to capture the onEvent handler the hook installs, so
 * the test can fire a server event and assert the right cache effect. Acquisition
 * completed/failed events PATCH caches (and drive the download store) rather than
 * invalidate (F12) — the detail save-control reads the library reactively, so the
 * old library-wide refetch on every finished download is gone.
 */

import { renderHook } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement } from 'react';
import type { ReactNode } from 'react';

import { useDownloadStore } from '@shared/acquisition/downloadStore';

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
  useDownloadStore.getState().reset();
});

describe('useServerEvents', () => {
  it('does not invalidate the library on completion — it patches (F12)', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '1', type: 'track_acquisition_completed', data: { track_id: 't1' } });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['library-home'] });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['library'] });
    expect(useDownloadStore.getState().entries['t1']?.phase).toBe('finishing');
  });

  it('does not invalidate the library on failure — it patches (F12)', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '2', type: 'track_acquisition_failed', data: { track_id: 't2' } });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ['library'] });
    expect(useDownloadStore.getState().entries['t2']?.phase).toBe('failed');
  });

  it('ignores event types that are not handled', () => {
    const invalidate = setup();
    captured.onEvent!({ id: '3', type: 'something_unmapped', data: {} });
    expect(invalidate).not.toHaveBeenCalled();
  });
});
