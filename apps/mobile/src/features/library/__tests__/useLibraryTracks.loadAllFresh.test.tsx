import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';

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

it('refetches the whole library on every loadAll', async () => {
  const a = { id: asTrackId('a') };
  const b = { id: asTrackId('b') };
  mockGetTracks.mockResolvedValue({ items: [a], total: 1, limit: 200, offset: 0, has_more: false });
  mockGetAllTracks.mockResolvedValueOnce([a, b]).mockResolvedValueOnce([a]);
  const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
  await waitFor(() => expect(result.current.tracks).toHaveLength(1));

  let first: unknown;
  let second: unknown;
  await act(async () => {
    first = await result.current.loadAll();
    second = await result.current.loadAll();
  });

  expect(first).toEqual([a, b]);
  expect(second).toEqual([a]);
  expect(mockGetAllTracks).toHaveBeenCalledTimes(2);
});
