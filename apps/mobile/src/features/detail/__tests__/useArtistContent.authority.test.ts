/**
 * useArtistContent — backend-validated discography ordering.
 *
 * When artistName is provided, the backend applies MB cross-reference
 * validation and returns confirmed albums first, unconfirmed last.
 * The hook must preserve this ordering (no client-side re-sort by date).
 * Without artistName, the hook sorts by release date descending.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useArtistContent } from '../hooks/useArtistContent';
import type { DiscoverySource } from '../../../shared/api-client/discovery';

const mockGetArtistAlbums = jest.fn();
const mockGetArtistTopTracks = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getArtistAlbums: (...args: unknown[]) => mockGetArtistAlbums(...args),
  getArtistTopTracks: (...args: unknown[]) => mockGetArtistTopTracks(...args),
}));

function _src(provider: string, externalId: string): DiscoverySource {
  return { provider, external_id: externalId, url: `https://x/${externalId}` };
}

function _album(title: string, provider: string, externalId: string, year?: string) {
  return {
    kind: 'album',
    title,
    subtitle: 'Che',
    image_url: null,
    confidence: 'low',
    sources: [_src(provider, externalId)],
    extras: year ? { year } : {},
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
  mockGetArtistAlbums.mockReset();
  mockGetArtistTopTracks
    .mockReset()
    .mockResolvedValue({ items: [], provider: 'deezer', status: 'ok', latency_ms: 0 });
});

const _SOURCES = [_src('deezer', '234701081')];

it('preserves backend ordering when artistName is provided', async () => {
  // Backend returns confirmed first (REST IN BASS), unconfirmed last (Samsonite)
  mockGetArtistAlbums.mockResolvedValue({
    items: [
      _album('REST IN BASS', 'deezer', 'alb-1', '2022'),
      _album('Sayso Says', 'deezer', 'alb-2', '2021'),
      _album('Samsonite', 'deezer', 'alb-3', '2023'), // unconfirmed, newer but last
    ],
    provider: 'deezer',
    status: 'ok',
    latency_ms: 0,
  });

  const { result } = renderHook(
    () => useArtistContent({ sources: _SOURCES, artistName: 'Che' }),
    { wrapper: _wrapper(_client()) },
  );

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  // Order preserved from backend — NOT re-sorted by date
  expect(result.current.albums.map((a) => a.title)).toEqual([
    'REST IN BASS',
    'Sayso Says',
    'Samsonite',
  ]);
});

it('sorts by release date when no artistName (no backend validation)', async () => {
  mockGetArtistAlbums.mockResolvedValue({
    items: [
      _album('Older', 'deezer', 'alb-1', '2020'),
      _album('Newer', 'deezer', 'alb-2', '2023'),
      _album('Middle', 'deezer', 'alb-3', '2021'),
    ],
    provider: 'deezer',
    status: 'ok',
    latency_ms: 0,
  });

  const { result } = renderHook(
    () => useArtistContent({ sources: _SOURCES }),
    { wrapper: _wrapper(_client()) },
  );

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  // Sorted by release date descending
  expect(result.current.albums.map((a) => a.title)).toEqual([
    'Newer',
    'Middle',
    'Older',
  ]);
});
