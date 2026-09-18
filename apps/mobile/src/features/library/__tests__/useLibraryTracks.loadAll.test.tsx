// #790: "shuffle/play whole library" resolves through loadAll. When the full-library
// fetch fails it falls back to the pages already loaded (playing a subset beats a tap
// that does nothing), but that degradation must leave a trace instead of being silent.

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

const loaded = [{ id: asTrackId('a') }, { id: asTrackId('b') }];

beforeEach(() => {
  mockGetTracks.mockReset();
  mockGetAllTracks.mockReset();
  mockGetTracks.mockResolvedValue({
    items: loaded,
    total: 5,
    limit: 200,
    offset: 0,
    has_more: true,
  });
});

describe('useLibraryTracks loadAll', () => {
  it('resolves the full library when the fetch succeeds', async () => {
    const all = [...loaded, { id: asTrackId('c') }];
    mockGetAllTracks.mockResolvedValue(all);
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(2));

    let resolved: unknown;
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(resolved).toEqual(all);
  });

  it('falls back to the loaded pages on failure and records that the fallback fired', async () => {
    mockGetAllTracks.mockRejectedValue(new Error('boom'));
    const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    const { result } = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
    await waitFor(() => expect(result.current.tracks).toHaveLength(2));

    let resolved: unknown;
    await act(async () => {
      resolved = await result.current.loadAll();
    });

    expect(resolved).toEqual(loaded);
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining('[library]'),
      expect.objectContaining({ loaded: 2 }),
    );
    warn.mockRestore();
  });
});
