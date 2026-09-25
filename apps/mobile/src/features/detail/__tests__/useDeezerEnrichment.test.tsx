import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetDetailHealthForTest } from '../detailHealth';
import { useDeezerEnrichment } from '../hooks/useDeezerEnrichment';

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

  const album = { kind: 'album', title: 'OK Computer', subtitle: 'Radiohead' } as const;

  describe.each([
    [
      'useDeezerEnrichment',
      'GET /v1/discovery/enrichment/deezer',
      (): void => void useDeezerEnrichment(album),
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
