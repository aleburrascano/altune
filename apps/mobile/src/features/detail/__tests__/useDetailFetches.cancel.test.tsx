// #2496: leaving a detail screen must abort its in-flight enrichment, artist-content,
// related-tracks and resolve requests instead of letting them run to the shared 15s
// deadline. TanStack only aborts on unmount when the queryFn consumed the context signal,
// so this drives the real api-client against the fetch double and checks the recorded
// request's signal.

import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import type { DiscoverySource } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { recordEvent } from '@shared/telemetry/recordEvent';

import { _resetDetailHealthForTest, flushDetailHealth } from '../detailHealth';

import { resolveEntityQuery } from '../resolve-entity-query';
import { useArtistContent } from '../hooks/useArtistContent';
import { useDeezerEnrichment } from '../hooks/useDeezerEnrichment';
import { useEnrichment } from '../hooks/useEnrichment';
import { useLastFmEnrichment } from '../hooks/useLastFmEnrichment';
import { useRelatedTracks } from '../hooks/useRelatedTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

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
  ['useEnrichment', 'GET /v1/discovery/enrichment', (): void => void useEnrichment(album)],
  [
    'useDeezerEnrichment',
    'GET /v1/discovery/enrichment/deezer',
    (): void => void useDeezerEnrichment(album),
  ],
  [
    'useLastFmEnrichment',
    'GET /v1/discovery/enrichment/lastfm',
    (): void => void useLastFmEnrichment(album),
  ],
  [
    'useArtistContent',
    'GET /v1/discovery/artists/spotify/a-1/content',
    (): void =>
      void useArtistContent({
        sources: [{ provider: 'spotify', external_id: 'a-1' } as DiscoverySource],
      }),
  ],
  [
    'useRelatedTracks',
    'GET /v1/discovery/tracks/soundcloud/sc-1/related',
    (): void =>
      void useRelatedTracks({
        sources: [{ provider: 'soundcloud', external_id: 'sc-1' } as DiscoverySource],
      }),
  ],
  [
    'resolveEntityQuery',
    'GET /v1/discovery/search',
    (): void => void useQuery(resolveEntityQuery('artist', 'radiohead', 1)),
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

describe('a fetch aborted by leaving the screen', () => {
  it('is tallied as neither success nor failure', async () => {
    __http.hang('GET /v1/discovery/enrichment');
    __http.hang('GET /v1/discovery/artists/spotify/a-1/content');
    const { unmount } = renderHook(
      () => {
        useEnrichment({ kind: 'album', title: 'OK Computer' });
        useArtistContent({
          sources: [{ provider: 'spotify', external_id: 'a-1' } as DiscoverySource],
        });
      },
      { wrapper },
    );
    await waitFor(() => expect(__http.requests).toHaveLength(2));

    unmount();
    await waitFor(() =>
      expect((__http.requests as { signal: AbortSignal }[]).every((r) => r.signal.aborted)).toBe(
        true,
      ),
    );
    flushDetailHealth();

    expect(recordEvent).not.toHaveBeenCalled();
  });
});
