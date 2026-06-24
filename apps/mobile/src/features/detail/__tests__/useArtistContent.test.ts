/**
 * useArtistContent — artistName passthrough to backend for MB validation.
 *
 * When artistName is provided, the hook passes it to getArtistAlbums so the
 * backend can cross-reference albums against MusicBrainz.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useArtistContent } from '../hooks/useArtistContent';
import type { ContentFetchResponse, DiscoverySource } from '../../../shared/api-client/discovery';

const mockGetArtistAlbums = jest.fn();
const mockGetArtistTopTracks = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getArtistAlbums: (...args: unknown[]) => mockGetArtistAlbums(...args),
  getArtistTopTracks: (...args: unknown[]) => mockGetArtistTopTracks(...args),
}));

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

describe('artistName passthrough', () => {
  it('passes artistName to getArtistAlbums for MB validation', async () => {
    const sources = [_src('deezer', 'dz-1')];
    renderHook(() => useArtistContent({ sources, artistName: 'Che' }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('deezer', 'dz-1', 100, 'Che');
  });

  it('omits artistName param when not provided', async () => {
    const sources = [_src('deezer', 'dz-1')];
    renderHook(() => useArtistContent({ sources }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('deezer', 'dz-1', 100, undefined);
  });

  it('queries only Deezer for albums (no MB content provider)', async () => {
    const sources = [_src('deezer', 'dz-1'), _src('musicbrainz', 'mb-1')];
    renderHook(() => useArtistContent({ sources, artistName: 'Che' }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(1));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('deezer', 'dz-1', 100, 'Che');
  });

  it('also fans out to iTunes for albums when an iTunes source is present', async () => {
    const sources = [_src('deezer', 'dz-1'), _src('itunes', '368183298')];
    renderHook(() => useArtistContent({ sources, artistName: 'Kendrick Lamar' }), {
      wrapper: _wrapper(_client()),
    });

    await waitFor(() => expect(mockGetArtistAlbums).toHaveBeenCalledTimes(2));
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('deezer', 'dz-1', 100, 'Kendrick Lamar');
    expect(mockGetArtistAlbums).toHaveBeenCalledWith('itunes', '368183298', 100, 'Kendrick Lamar');
  });
});
