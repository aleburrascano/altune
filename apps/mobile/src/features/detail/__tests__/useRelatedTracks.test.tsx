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
