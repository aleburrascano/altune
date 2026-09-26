import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import { isRetryable } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import type { DiscoverySource } from '@shared/api-client/discovery';

import { useAlbumTracks } from '../hooks/useAlbumTracks';
import { useArtistContent } from '../hooks/useArtistContent';
import { useRelatedTracks } from '../hooks/useRelatedTracks';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

// Since #1102 a failed content fetch is a non-2xx with a discovery.* code. The
// app-wide query retry (app/_layout.tsx) retries 5xx five times with backoff,
// so the detail screen spun ~30s before showing its retry UI. These hooks must
// surface the error after a single request instead.

// The same default the app installs, so the test proves the per-hook override.
function appQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: (failureCount, error) => isRetryable(error) && failureCount < 5 },
    },
  });
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const ALBUM_PATH = 'GET /v1/discovery/albums/spotify/album-1/tracks';
const ARTIST_PATH = 'GET /v1/discovery/artists/spotify/artist-1/content';
const RELATED_PATH = 'GET /v1/discovery/tracks/soundcloud/sc-1/related';

const artistSources: DiscoverySource[] = [
  { provider: 'spotify', external_id: 'artist-1' } as DiscoverySource,
];
const trackSources: DiscoverySource[] = [
  { provider: 'soundcloud', external_id: 'sc-1' } as DiscoverySource,
];

const hooks = [
  {
    name: 'useAlbumTracks',
    path: ALBUM_PATH,
    use: () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).isError,
    useFailure: () => useAlbumTracks({ provider: 'spotify', externalId: 'album-1' }).failure,
  },
  {
    name: 'useArtistContent',
    path: ARTIST_PATH,
    use: () => useArtistContent({ sources: artistSources, artistName: 'A' }).tracksFailure !== null,
    useFailure: () => useArtistContent({ sources: artistSources, artistName: 'A' }).tracksFailure,
  },
  {
    name: 'useRelatedTracks',
    path: RELATED_PATH,
    use: () => useRelatedTracks({ sources: trackSources }).isError,
    useFailure: () => useRelatedTracks({ sources: trackSources }).failure,
  },
];

const settledFailures = [
  { status: 503, code: 'discovery.provider_circuit_open' },
  { status: 502, code: 'discovery.provider_error' },
  { status: 504, code: 'discovery.provider_timeout' },
  { status: 503, code: 'discovery.provider_rate_limited' },
];

// Well past the app-wide backoff (1+2+4+8+16s) so any retry would have fired.
async function renderWithPastBackoff<T>(use: () => T): Promise<T> {
  const queryClient = appQueryClient();
  const { result, unmount } = renderHook(use, { wrapper: createWrapper(queryClient) });
  await act(async () => {
    await jest.advanceTimersByTimeAsync(60_000);
  });
  const state = result.current;
  unmount();
  queryClient.clear();
  return state;
}

let warn: jest.SpyInstance;

beforeEach(() => {
  jest.useFakeTimers();
  warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  (supabase.auth.getSession as jest.Mock).mockReset().mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  jest.useRealTimers();
  warn.mockRestore();
});

describe.each(hooks)('$name fails fast on a settled content failure', ({ path, use }) => {
  it.each(settledFailures)('shows the error after one request for $status $code', async (f) => {
    __http.reply(path, { status: f.status, json: { status: 'error', code: f.code } });

    const isError = await renderWithPastBackoff(use);

    expect(isError).toBe(true);
    expect(__http.countFor(path)).toBe(1);
  });

  it('still retries a 5xx without a discovery code', async () => {
    __http.replyOnce(path, { status: 500, json: { code: 'internal' } });
    __http.reply(path, { status: 500, json: { code: 'internal' } });

    await renderWithPastBackoff(use);

    expect(__http.countFor(path)).toBeGreaterThan(1);
  });
});

// #1663: the same classification the fail-fast policy reads must reach the
// caller, so a section can tell "asking again is pointless" from "worth a tap".
describe.each(hooks)('$name reports why the content fetch failed', ({ path, useFailure }) => {
  it.each(settledFailures)('classifies $status $code as settled', async (f) => {
    __http.reply(path, { status: f.status, json: { status: 'error', code: f.code } });

    expect(await renderWithPastBackoff(useFailure)).toBe('settled');
  });

  it('classifies an unreachable server as transient', async () => {
    __http.fail(path);

    expect(await renderWithPastBackoff(useFailure)).toBe('transient');
  });
});
