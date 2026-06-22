/**
 * useEnrichment — detail-open MusicBrainz enrichment fetch (musicbrainz-enrichment spec).
 * Renders the hook against a real QueryClient; getEnrichment is mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const mockGetEnrichment = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getEnrichment: (...args: unknown[]) => mockGetEnrichment(...args),
}));

import { useEnrichment } from '../hooks/useEnrichment';
import type { EnrichmentResponse } from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<EnrichmentResponse> = {}): EnrichmentResponse {
  return {
    mbid: 'mbid-1',
    genres: ['hip hop'],
    year: 2017,
    rating: 4.1,
    rating_votes: 7,
    primary_type: 'Album',
    secondary_types: [],
    external_ids: { deezer: '525046' },
    artwork_url: 'https://caa/1200.jpg',
    ...over,
  };
}

const _empty: EnrichmentResponse = {
  mbid: '',
  genres: [],
  year: 0,
  rating: 0,
  rating_votes: 0,
  primary_type: '',
  secondary_types: [],
  external_ids: {},
  artwork_url: '',
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGetEnrichment.mockReset();
});

describe('useEnrichment', () => {
  it('fetches and returns enrichment for a titled result', async () => {
    mockGetEnrichment.mockResolvedValueOnce(_enrichment());

    const { result } = renderHook(
      () => useEnrichment({ kind: 'album', title: 'DAMN.', subtitle: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.enrichment).not.toBeNull());
    expect(result.current.enrichment?.genres).toEqual(['hip hop']);
    expect(result.current.enrichment?.year).toBe(2017);
    expect(mockGetEnrichment).toHaveBeenCalledWith({
      kind: 'album',
      title: 'DAMN.',
      subtitle: 'Kendrick Lamar',
      mbid: undefined,
    });
    expect(result.current.isError).toBe(false);
  });

  it('treats an empty payload as no enrichment', async () => {
    mockGetEnrichment.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useEnrichment({ kind: 'track', title: 'Obscure Bootleg' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('does not fetch when there is no title and no mbid', async () => {
    const { result } = renderHook(() => useEnrichment({ kind: 'album', title: '' }), {
      wrapper: _wrapper(),
    });

    expect(mockGetEnrichment).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
    expect(result.current.isLoading).toBe(false);
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGetEnrichment.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useEnrichment({ kind: 'album', title: 'DAMN.', subtitle: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.enrichment).toBeNull();
  });
});
