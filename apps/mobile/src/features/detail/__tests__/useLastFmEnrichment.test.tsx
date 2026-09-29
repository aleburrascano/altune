import type { QueryClient } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetDetailHealthForTest } from '../detailHealth';
import { useLastFmEnrichment } from '../hooks/useLastFmEnrichment';
import { createTestQueryClient, createWrapper, mockSupabaseSession } from './support/queryHarness';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

describe('aborting on unmount', () => {
  let client: QueryClient;

  let wrapper: ReturnType<typeof createWrapper>;

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue(mockSupabaseSession());
    _resetDetailHealthForTest();
    (recordEvent as jest.Mock).mockReset().mockResolvedValue(undefined);
    client = createTestQueryClient();
    wrapper = createWrapper(client);
  });

  afterEach(() => client.clear());

  const album = { kind: 'album', title: 'OK Computer', subtitle: 'Radiohead' } as const;

  describe.each([
    [
      'useLastFmEnrichment',
      'GET /v1/discovery/enrichment/lastfm',
      (): void => void useLastFmEnrichment(album),
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
