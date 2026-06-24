/**
 * useDiscogsArtistEnrichment — detail-open Discogs artist enrichment fetch
 * (docs/providers/discogs.md cap 7). Real QueryClient; getDiscogsArtistEnrichment
 * is mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

import { useDiscogsArtistEnrichment } from '../hooks/useDiscogsArtistEnrichment';
import type { DiscogsArtistEnrichmentResponse } from '../../../shared/api-client/discovery';

const mockGet = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getDiscogsArtistEnrichment: (...args: unknown[]) => mockGet(...args),
}));

function _enrichment(
  over: Partial<DiscogsArtistEnrichmentResponse> = {},
): DiscogsArtistEnrichmentResponse {
  return {
    artist_id: 3062364,
    profile: 'American rapper.',
    real_name: 'Kendrick Lamar Duckworth',
    aliases: ['K Dot (2)'],
    name_variations: [],
    members: [],
    groups: ['Black Hippy'],
    links: [{ label: 'Wikipedia', url: 'https://en.wikipedia.org/wiki/Kendrick_Lamar' }],
    ...over,
  };
}

const _empty: DiscogsArtistEnrichmentResponse = {
  artist_id: 0,
  profile: '',
  real_name: '',
  aliases: [],
  name_variations: [],
  members: [],
  groups: [],
  links: [],
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGet.mockReset();
});

describe('useDiscogsArtistEnrichment', () => {
  it('fetches and returns enrichment for an artist', async () => {
    mockGet.mockResolvedValueOnce(_enrichment());

    const { result } = renderHook(
      () => useDiscogsArtistEnrichment({ name: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.enrichment).not.toBeNull());
    expect(result.current.enrichment?.groups).toEqual(['Black Hippy']);
    expect(mockGet).toHaveBeenCalledWith({ name: 'Kendrick Lamar' });
  });

  it('treats an empty payload as no enrichment', async () => {
    mockGet.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useDiscogsArtistEnrichment({ name: 'Nobody' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('does not fetch when disabled', async () => {
    const { result } = renderHook(
      () => useDiscogsArtistEnrichment({ name: 'Kendrick Lamar', enabled: false }),
      { wrapper: _wrapper() },
    );

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useDiscogsArtistEnrichment({ name: 'Kendrick Lamar' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.enrichment).toBeNull();
  });
});
