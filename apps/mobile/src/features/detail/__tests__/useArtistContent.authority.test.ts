/**
 * useArtistContent — MB-authoritative discography.
 *
 * Deezer's artist entities can conflate several same-name artists (its
 * /artist/{id}/albums for "Che" mixes a 1990 EP and German/Spanish singles
 * from unrelated artists, with no per-album artist field to filter on).
 * When the artist's identity is verified (mbid matches an MB source) and MB
 * returned a healthy discography, Deezer only ENRICHES title-matched albums
 * (sources/track counts) and contributes no new titles. Without a verified
 * identity, the full union stands — Deezer may be all we have.
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

function _album(title: string, provider: string, externalId: string, trackCount?: number) {
  return {
    kind: 'album',
    title,
    subtitle: 'Che',
    image_url: null,
    confidence: 'low',
    sources: [_src(provider, externalId)],
    extras: trackCount !== undefined ? { track_count: trackCount } : {},
  };
}

function _wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

function _client(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function _mockAlbums(mbItems: unknown[], dzItems: unknown[]): void {
  mockGetArtistAlbums.mockImplementation((provider: string) =>
    Promise.resolve({
      items: provider === 'musicbrainz' ? mbItems : dzItems,
      provider,
      status: 'ok',
      latency_ms: 0,
    }),
  );
}

beforeEach(() => {
  mockGetArtistAlbums.mockReset();
  mockGetArtistTopTracks
    .mockReset()
    .mockResolvedValue({ items: [], provider: 'deezer', status: 'ok', latency_ms: 0 });
});

const _SOURCES = [_src('deezer', '234701081'), _src('musicbrainz', _MBID)];

it('drops Deezer-only titles when identity is verified and MB is healthy', async () => {
  _mockAlbums(
    [_album('The Final Agenda', 'musicbrainz', 'rg-1')],
    [_album('Lande immer bei dir', 'deezer', 'alb-foreign')],
  );
  const { result } = renderHook(() => useArtistContent({ sources: _SOURCES, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  expect(result.current.albums.map((a) => a.title)).toEqual(['The Final Agenda']);
});

it('still enriches MB albums with title-matched Deezer data', async () => {
  _mockAlbums(
    [_album('Sad Lite', 'musicbrainz', 'rg-2', 3)],
    [_album('Sad Lite', 'deezer', 'alb-2', 5), _album('Lande immer bei dir', 'deezer', 'alb-f')],
  );
  const { result } = renderHook(() => useArtistContent({ sources: _SOURCES, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  expect(result.current.albums).toHaveLength(1);
  const album = result.current.albums[0]!;
  expect(album.extras['track_count']).toBe(5); // higher Deezer count wins
  expect(album.sources.map((s) => s.provider).sort()).toEqual(['deezer', 'musicbrainz']);
});

it('keeps the full union when no verified identity exists', async () => {
  _mockAlbums(
    [_album('The Final Agenda', 'musicbrainz', 'rg-1')],
    [_album('Deezer Only Single', 'deezer', 'alb-3')],
  );
  const { result } = renderHook(() => useArtistContent({ sources: _SOURCES, mbid: null }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  expect(result.current.albums.map((a) => a.title).sort()).toEqual([
    'Deezer Only Single',
    'The Final Agenda',
  ]);
});

it('falls back to the Deezer list when MB returns ok but empty', async () => {
  _mockAlbums([], [_album('Deezer Only Single', 'deezer', 'alb-3')]);
  const { result } = renderHook(() => useArtistContent({ sources: _SOURCES, mbid: _MBID }), {
    wrapper: _wrapper(_client()),
  });

  await waitFor(() => expect(result.current.isLoadingAlbums).toBe(false));
  expect(result.current.albums.map((a) => a.title)).toEqual(['Deezer Only Single']);
});
