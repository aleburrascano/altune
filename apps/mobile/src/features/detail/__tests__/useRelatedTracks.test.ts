/**
 * useRelatedTracks — SoundCloud-gated related-tracks fetch (related-tracks spec).
 * Renders the hook against a real QueryClient; getRelatedTracks is mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const mockGetRelatedTracks = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getRelatedTracks: (...args: unknown[]) => mockGetRelatedTracks(...args),
}));

import { useRelatedTracks } from '../hooks/useRelatedTracks';
import type {
  ContentFetchResponse,
  DiscoveryResult,
  DiscoverySource,
} from '../../../shared/api-client/discovery';

function _track(title: string): DiscoveryResult {
  return {
    kind: 'track',
    title,
    subtitle: 'An Artist',
    image_url: null,
    confidence: 'low',
    sources: [{ provider: 'soundcloud', external_id: '1', url: 'https://sc/x' }],
    extras: {},
  };
}

function _ok(items: DiscoveryResult[]): ContentFetchResponse {
  return { items, provider: 'soundcloud', status: 'ok', latency_ms: 1 };
}

const _scSource: DiscoverySource = {
  provider: 'soundcloud',
  external_id: '12345',
  url: 'https://soundcloud.com/x/seed',
};

const _deezerSource: DiscoverySource = {
  provider: 'deezer',
  external_id: '999',
  url: 'https://deezer.com/999',
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGetRelatedTracks.mockReset();
});

describe('useRelatedTracks', () => {
  it('fetches and returns items for a SoundCloud-sourced track', async () => {
    mockGetRelatedTracks.mockResolvedValueOnce(_ok([_track('Fell In Love')]));

    const { result } = renderHook(() => useRelatedTracks({ sources: [_scSource] }), {
      wrapper: _wrapper(),
    });

    await waitFor(() => expect(result.current.relatedTracks).toHaveLength(1));
    expect(result.current.relatedTracks[0].title).toBe('Fell In Love');
    expect(mockGetRelatedTracks).toHaveBeenCalledWith('soundcloud', '12345', 20);
    expect(result.current.isError).toBe(false);
  });

  it('does not fetch when the track has no SoundCloud source', async () => {
    const { result } = renderHook(() => useRelatedTracks({ sources: [_deezerSource] }), {
      wrapper: _wrapper(),
    });

    // Query disabled: no fetch, empty rail, not loading.
    expect(mockGetRelatedTracks).not.toHaveBeenCalled();
    expect(result.current.relatedTracks).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  it('surfaces no items and isError when the provider payload is not ok', async () => {
    mockGetRelatedTracks.mockResolvedValueOnce({
      items: [],
      provider: 'soundcloud',
      status: 'error',
      latency_ms: 1,
    } satisfies ContentFetchResponse);

    const { result } = renderHook(() => useRelatedTracks({ sources: [_scSource] }), {
      wrapper: _wrapper(),
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.relatedTracks).toEqual([]);
  });
});
