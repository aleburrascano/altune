/**
 * useArtistContent — MB source selection via the artist's authoritative mbid.
 *
 * The merged artist card can carry several same-name MusicBrainz sources;
 * extras.mbid (resolved by the backend) identifies the right one. Renders the
 * hook against a real QueryClient; the discovery api-client is mocked.
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
import type { ContentFetchResponse, DiscoverySource } from '../../../shared/api-client/discovery';

const _RIGHT_MBID = '0a68f3b5-79c2-4f81-a7bc-ebc977602e86';
const _WRONG_MBID = '79d5ff17-0000-0000-0000-000000000000';

function _src(provider: string, externalId: string): DiscoverySource {
  return { provider, external_id: externalId, url: `https://x/${externalId}` };
}

function _ok(provider: string): ContentFetchResponse {
  return { items: [], provider, status: 'ok', latency_ms: 0 };
}

function _wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

function _client(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

beforeEach(() => {
  mockGetArtistAlbums.mockReset().mockImplementation((provider: string) => Promise.resolve(_ok(provider)));
  mockGetArtistTopTracks.mockReset().mockResolvedValue(_ok('deezer'));
});

describe('mbid-directed MB source selection', () => {
  it('queries the MB source whose external_id matches extras.mbid', async () => {
    const sources = [
      _src('deezer', 'dz-1'),
      _src('musicbrainz', _WRONG_MBID),
      _src('musicbrainz', _RIGHT_MBID),
    ];
    renderHook(() => useArtistContent({ sources, mbid: _RIGHT_MBID }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(2));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('musicbrainz', _RIGHT_MBID, 100);
    expect(mockGetArtistAlbums).not.toHaveBeenCalledWith('musicbrainz', _WRONG_MBID, 100);
  });

  it('falls back to the first MB source when mbid is null', async () => {
    const sources = [
      _src('musicbrainz', _WRONG_MBID),
      _src('musicbrainz', _RIGHT_MBID),
    ];
    renderHook(() => useArtistContent({ sources, mbid: null }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('musicbrainz', _WRONG_MBID, 100);
  });

  it('falls back to the first MB source when mbid matches no source', async () => {
    const sources = [_src('musicbrainz', _WRONG_MBID)];
    renderHook(() => useArtistContent({ sources, mbid: _RIGHT_MBID }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('musicbrainz', _WRONG_MBID, 100);
  });
});
