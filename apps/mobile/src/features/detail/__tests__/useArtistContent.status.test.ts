/**
 * useArtistContent — per-provider status handling.
 *
 * The backend returns HTTP 200 with status timeout/error and empty items;
 * the hook must treat a non-ok payload as that provider's failure instead
 * of silently showing a partial (e.g. Deezer-only) discography.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const mockGetArtistAlbums = jest.fn();
const mockGetArtistTopTracks = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getArtistAlbums: (...args: unknown[]) => mockGetArtistAlbums(...args),
  getArtistTopTracks: (...args: unknown[]) => mockGetArtistTopTracks(...args),
}));

import { useArtistContent } from '../hooks/useArtistContent';
import type { DiscoverySource } from '../../../shared/api-client/discovery';

const _MBID = '0a68f3b5-79c2-4f81-a7bc-ebc977602e86';

function _src(provider: string, externalId: string): DiscoverySource {
  return { provider, external_id: externalId, url: `https://x/${externalId}` };
}

function _album(title: string, provider: string, externalId: string) {
  return {
    kind: 'album',
    title,
    subtitle: 'Che',
    image_url: null,
    confidence: 'low',
    sources: [_src(provider, externalId)],
    extras: {},
  };
}

function _wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

function _client(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => {
  mockGetArtistAlbums
    .mockReset()
    .mockImplementation((provider: string) =>
      Promise.resolve({ items: [], provider, status: 'ok', latency_ms: 0 }),
    );
  mockGetArtistTopTracks
    .mockReset()
    .mockResolvedValue({ items: [], provider: 'deezer', status: 'ok', latency_ms: 0 });
});

it('keeps Deezer albums and no error when only MB times out (HTTP 200 payload)', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve(
      provider === 'musicbrainz'
        ? { items: [], provider, status: 'timeout', latency_ms: 0 }
        : {
            items: [_album('Sad Lite', 'deezer', 'alb-1')],
            provider,
            status: 'ok',
            latency_ms: 0,
          },
    ),
  );
  const sources = [_src('deezer', 'dz-1'), _src('musicbrainz', _MBID)];
  const { result } = renderHook(() => useArtistContent({ sources, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.albums).toHaveLength(1));
  expect(result.current.albums[0]?.title).toBe('Sad Lite');
  expect(result.current.isErrorAlbums).toBe(false);
});

it('flags isErrorAlbums when both providers return non-ok payloads', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve({
      items: [],
      provider,
      status: provider === 'deezer' ? 'error' : 'timeout',
      latency_ms: 0,
    }),
  );
  const sources = [_src('deezer', 'dz-1'), _src('musicbrainz', _MBID)];
  const { result } = renderHook(() => useArtistContent({ sources, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isErrorAlbums).toBe(true));
});

it('ignores items from a non-ok payload', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve(
      provider === 'musicbrainz'
        ? {
            items: [_album('Ghost', 'musicbrainz', 'rg-1')],
            provider,
            status: 'error',
            latency_ms: 0,
          }
        : { items: [], provider, status: 'ok', latency_ms: 0 },
    ),
  );
  const sources = [_src('deezer', 'dz-1'), _src('musicbrainz', _MBID)];
  const { result } = renderHook(() => useArtistContent({ sources, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  expect(result.current.albums).toHaveLength(0);
});

it('flags isErrorTracks on a timeout payload (not only error)', async () => {
  mockGetArtistTopTracks.mockResolvedValue({
    items: [],
    provider: 'deezer',
    status: 'timeout',
    latency_ms: 0,
  });
  const sources = [_src('deezer', 'dz-1')];
  const { result } = renderHook(() => useArtistContent({ sources, mbid: null }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isErrorTracks).toBe(true));
});
