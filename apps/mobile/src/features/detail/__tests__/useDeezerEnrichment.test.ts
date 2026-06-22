/**
 * useDeezerEnrichment — detail-open Deezer enrichment fetch
 * (docs/providers/deezer.md caps 7–8). Real QueryClient; getDeezerEnrichment mocked.
 */
import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

const mockGet = jest.fn();
jest.mock('../../../shared/api-client/discovery', () => ({
  getDeezerEnrichment: (...args: unknown[]) => mockGet(...args),
}));

import { useDeezerEnrichment } from '../hooks/useDeezerEnrichment';
import type { DeezerEnrichmentResponse } from '../../../shared/api-client/discovery';

function _enrichment(over: Partial<DeezerEnrichmentResponse> = {}): DeezerEnrichmentResponse {
  return {
    bpm: 172,
    gain: -8.3,
    explicit: true,
    label: '',
    genres: [],
    upc: '',
    record_type: '',
    ...over,
  };
}

const _empty: DeezerEnrichmentResponse = {
  bpm: 0,
  gain: 0,
  explicit: false,
  label: '',
  genres: [],
  upc: '',
  record_type: '',
};

function _wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

afterEach(() => {
  mockGet.mockReset();
});

describe('useDeezerEnrichment', () => {
  it('fetches and returns enrichment for a track', async () => {
    mockGet.mockResolvedValueOnce(_enrichment());

    const { result } = renderHook(
      () => useDeezerEnrichment({ kind: 'track', title: 'Lose Yourself', subtitle: 'Eminem' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.enrichment).not.toBeNull());
    expect(result.current.enrichment?.bpm).toBe(172);
    expect(mockGet).toHaveBeenCalledWith({
      kind: 'track',
      title: 'Lose Yourself',
      subtitle: 'Eminem',
    });
  });

  it('treats an empty payload as no enrichment', async () => {
    mockGet.mockResolvedValueOnce(_empty);

    const { result } = renderHook(
      () => useDeezerEnrichment({ kind: 'album', title: 'Nothing', subtitle: 'Nobody' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('treats a gain-only payload as no enrichment (gain is not displayed)', async () => {
    mockGet.mockResolvedValueOnce(_enrichment({ bpm: 0, explicit: false, gain: -9.1 }));

    const { result } = renderHook(
      () => useDeezerEnrichment({ kind: 'track', title: 'Instrumental', subtitle: 'Someone' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.enrichment).toBeNull();
  });

  it('does not fetch when disabled', () => {
    const { result } = renderHook(
      () => useDeezerEnrichment({ kind: 'artist', title: 'Eminem', subtitle: null, enabled: false }),
      { wrapper: _wrapper() },
    );

    expect(mockGet).not.toHaveBeenCalled();
    expect(result.current.enrichment).toBeNull();
  });

  it('surfaces isError without throwing when the request fails', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'));

    const { result } = renderHook(
      () => useDeezerEnrichment({ kind: 'track', title: 'Lose Yourself', subtitle: 'Eminem' }),
      { wrapper: _wrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.enrichment).toBeNull();
  });
});
