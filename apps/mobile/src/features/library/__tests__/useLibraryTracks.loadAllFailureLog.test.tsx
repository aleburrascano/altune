import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import { ApiError } from '@shared/errors';

import { useLibraryTracks } from '../hooks/useLibraryTracks';

const mockGetTracks = jest.fn();
const mockGetAllTracks = jest.fn();
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: unknown) => mockGetTracks(params),
  getAllTracks: (params: unknown) => mockGetAllTracks(params),
}));

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe('useLibraryTracks loadAll failure log', () => {
  it('logs status, failure class and correlation id when the fetch is rejected', async () => {
    mockGetTracks.mockResolvedValue({
      items: [{ id: asTrackId('a') }],
      total: 5,
      limit: 200,
      offset: 0,
      has_more: true,
    });
    mockGetAllTracks.mockRejectedValue(new ApiError(503, 'unavailable', undefined, 'corr-1'));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(1));

    await act(async () => {
      await result.current.loadAll();
    });

    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({
        loaded: 1,
        status: 503,
        failure: expect.any(String),
        correlationId: 'corr-1',
      }),
    );
    warn.mockRestore();
  });
});
