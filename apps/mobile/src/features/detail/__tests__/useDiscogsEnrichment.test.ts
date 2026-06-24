/**
 * useDiscogsEnrichment — detail-open Discogs album enrichment fetch
 * (docs/providers/discogs.md caps 3–6). Real QueryClient; getDiscogsEnrichment
 * is mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useDiscogsEnrichment } from '../hooks/useDiscogsEnrichment';
import type { DiscogsEnrichmentResponse } from '../../../shared/api-client/discovery';

const mockGet = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getDiscogsEnrichment: (...args: unknown[]) => mockGet(...args),
}));

function _enrichment(over: Partial<DiscogsEnrichmentResponse> = {}): DiscogsEnrichmentResponse {
  return {
    master_id: 1164779,
    genres: ['Hip Hop'],
    styles: ['Conscious'],
    year: 2017,
    credits: [{ name: 'Bēkon', role: 'Producer' }],
    labels: [{ name: 'Top Dawg Entertainment', catno: 'B0026716-02' }],
    formats: ['CD · Album'],
    country: 'US',
    companies: [],
    community: { have: 2980, want: 1946, rating: 4.27, votes: 313 },
    ...over,
  };
}

const _empty: DiscogsEnrichmentResponse = {
  master_id: 0,
  genres: [],
  styles: [],
  year: 0,
  credits: [],
  labels: [],
  formats: [],
  country: '',
  companies: [],
  community: { have: 0, want: 0, rating: 0, votes: 0 },
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGet.mockReset();
});

describe('useDiscogsEnrichment', () => {
  it('fetches and returns enrichment for an album', async () => {
    mockGet.mockResolvedValueOnce(_enrichment());

    const { result } = renderHook(
      () => useDiscogsEnrichment({ album: 'DAMN.', artist: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.enrichment).not.toBeNull());
    expect(result.current.enrichment?.styles).toEqual(['Conscious']);
    expect(result.current.enrichment?.credits[0]?.name).toBe('Bēkon');
    expect(mockGet).toHaveBeenCalledWith({ album: 'DAMN.', artist: 'Kendrick Lamar' });
    expect(result.current.isError).toBe(false);
  });

  it('treats an empty payload as no enrichment', async () => {
    mockGet.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useDiscogsEnrichment({ album: 'Obscure Bootleg', artist: null }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('does not fetch when disabled', async () => {
    const { result } = renderHook(
      () => useDiscogsEnrichment({ album: 'DAMN.', artist: 'Kendrick Lamar', enabled: false }),
      { wrapper: _wrapper() },
    );

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
    expect(result.current.isLoading).toBe(false);
  });

  it('does not fetch when the album is blank', async () => {
    const { result } = renderHook(() => useDiscogsEnrichment({ album: '   ' }), {
      wrapper: _wrapper(),
    });

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useDiscogsEnrichment({ album: 'DAMN.', artist: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.enrichment).toBeNull();
  });
});
