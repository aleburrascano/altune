/**
 * useArtistContent — per-provider status handling.
 *
 * The backend returns HTTP 200 with status timeout/error and empty items;
 * the hook must treat a non-ok payload as that provider's failure instead
 * of silently showing a partial discography.
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

it('shows Deezer albums when provider returns ok', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve({
      items: [_album('Sad Lite', 'deezer', 'alb-1')],
      provider,
      status: 'ok',
      latency_ms: 0,
    }),
  );
  const sources = [_src('deezer', 'dz-1')];
  const { result } = renderHook(() => useArtistContent({ sources, artistName: 'Che' }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.albums).toHaveLength(1));
  expect(result.current.albums[0]?.title).toBe('Sad Lite');
  expect(result.current.isErrorAlbums).toBe(false);
});

it('flags isErrorAlbums when Deezer returns non-ok payload', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve({
      items: [],
      provider,
      status: 'error',
      latency_ms: 0,
    }),
  );
  const sources = [_src('deezer', 'dz-1')];
  const { result } = renderHook(() => useArtistContent({ sources, artistName: 'Che' }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isErrorAlbums).toBe(true));
});

it('ignores items from a non-ok payload', async () => {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve({
      items: [_album('Ghost', 'deezer', 'alb-1')],
      provider,
      status: 'error',
      latency_ms: 0,
    }),
  );
  const sources = [_src('deezer', 'dz-1')];
  const { result } = renderHook(() => useArtistContent({ sources, artistName: 'Che' }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
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
  const { result } = renderHook(() => useArtistContent({ sources }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isErrorTracks).toBe(true));
});
