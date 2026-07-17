/**
 * useLastFmEnrichment — detail-open Last.fm enrichment fetch
 * (docs/providers/lastfm.md cap 3). Real QueryClient; getLastFmEnrichment mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useLastFmEnrichment } from '../hooks/useLastFmEnrichment';
import type { LastFmEnrichmentResponse } from '../../../shared/api-client/enrichment';

const mockGet = jest.fn();
jest.mock('../../../shared/api-client/enrichment', () => ({
  getLastFmEnrichment: (...args: unknown[]) => mockGet(...args),
}));

function _enrichment(over: Partial<LastFmEnrichmentResponse> = {}): LastFmEnrichmentResponse {
  return {
    mbid: '381086ea-f511-4aba-bdf9-71c753dc5077',
    listeners: 5172275,
    playcount: 1050884806,
    tags: ['Hip-Hop', 'rap'],
    bio: '',
    similar: ['Baby Keem', 'Jay Rock'],
    duration: 0,
    album: '',
    ...over,
  };
}

const _empty: LastFmEnrichmentResponse = {
  mbid: '',
  listeners: 0,
  playcount: 0,
  tags: [],
  bio: '',
  similar: [],
  duration: 0,
  album: '',
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGet.mockReset();
});

describe('useLastFmEnrichment', () => {
  it('fetches and returns enrichment for an artist', async () => {
    mockGet.mockResolvedValueOnce(_enrichment());

    const { result } = renderHook(
      () => useLastFmEnrichment({ kind: 'artist', title: 'Kendrick Lamar', subtitle: null }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.enrichment).not.toBeNull());
    expect(result.current.enrichment?.listeners).toBe(5172275);
    expect(result.current.enrichment?.similar).toEqual(['Baby Keem', 'Jay Rock']);
    expect(mockGet).toHaveBeenCalledWith({
      kind: 'artist',
      title: 'Kendrick Lamar',
      subtitle: null,
    });
  });

  it('treats an empty payload as no enrichment', async () => {
    mockGet.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useLastFmEnrichment({ kind: 'artist', title: 'Nobody', subtitle: null }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('does not fetch when the title is blank', () => {
    const { result } = renderHook(
      () => useLastFmEnrichment({ kind: 'track', title: '   ', subtitle: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useLastFmEnrichment({ kind: 'artist', title: 'Kendrick Lamar', subtitle: null }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.enrichment).toBeNull();
  });
});
