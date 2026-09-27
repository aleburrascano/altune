import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import type { DiscoverySource } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetDetailHealthForTest } from '../detailHealth';
import { useRelatedTracks } from '../hooks/useRelatedTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

describe('aborting on unmount', () => {
  let client: QueryClient;

  function wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    _resetDetailHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => client.clear());

  describe.each([
    [
      'useRelatedTracks',
      'GET /v1/discovery/tracks/soundcloud/sc-1/related',
      (): void =>
        void useRelatedTracks({
          sources: [{ provider: 'soundcloud', external_id: 'sc-1' } as DiscoverySource],
        }),
    ],
  ] as const)('%s, left before its request resolves', (_name, spec, useHook) => {
    it('aborts the in-flight request when the screen unmounts', async () => {
      __http.hang(spec);
      const { unmount } = renderHook(useHook, { wrapper });
      await waitFor(() => expect(__http.last()).toBeDefined());
      const { signal } = __http.last() as { signal: AbortSignal };
      expect(signal.aborted).toBe(false);

      unmount();

      await waitFor(() => expect(signal.aborted).toBe(true));
    });
  });
});

describe('reopening related tracks against the 30-minute content cache window', () => {
  const RELATED_PATH = 'GET /v1/discovery/tracks/soundcloud/sc-cache/related';
  const sources = [{ provider: 'soundcloud', external_id: 'sc-cache' } as DiscoverySource];
  const { act } = require('@testing-library/react-native');

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    _resetDetailHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
  });

  it.each([
    ['serves the cached list without a request 1 ms before 30 minutes', 1_799_999, 1],
    ['asks the server again 1 ms after 30 minutes', 1_800_001, 2],
  ])('%s', async (_behaviour, elapsedMs, expectedRequests) => {
    let now = 1_700_000_000_000;
    jest.spyOn(Date, 'now').mockImplementation(() => now);
    __http.reply(RELATED_PATH, {
      status: 200,
      json: { items: [], provider_name: 'soundcloud', status: 'ok' },
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const first = renderHook(() => useRelatedTracks({ sources }), { wrapper });
    await waitFor(() => expect(first.result.current.isLoading).toBe(false));
    expect(first.result.current.isError).toBe(false);
    first.unmount();
    now += elapsedMs;
    renderHook(() => useRelatedTracks({ sources }), { wrapper });
    await act(async () => {
      await new Promise((resolve) => setImmediate(resolve));
    });

    expect(__http.countFor(RELATED_PATH)).toBe(expectedRequests);
    client.clear();
  });
});
