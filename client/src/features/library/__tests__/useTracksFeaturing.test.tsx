import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import type { FeaturedArtist } from '@shared/api-client/trackResponse';

import { createMemoryLibraryPort } from '@features/library/__tests__/memoryLibraryPort';
import { LibraryPortProvider } from '@features/library/ports/LibraryPortProvider';

import {
  parseDeezerIdParam,
  parseStringParam,
  useTracksFeaturing,
} from '../hooks/useTracksFeaturing';

const memory = createMemoryLibraryPort();
const mockListTracksFeaturing = jest.fn(memory.listTracksFeaturing);
const port = { ...memory, listTracksFeaturing: mockListTracksFeaturing };

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={client}>
      <LibraryPortProvider port={port}>{children}</LibraryPortProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  mockListTracksFeaturing.mockClear();
});

describe('parseDeezerIdParam', () => {
  it.each([
    ['42', 42],
    ['3135556', 3135556],
  ])('parses %p to %p', (raw, expected) => {
    expect(parseDeezerIdParam(raw)).toBe(expected);
  });

  it.each([
    undefined,
    '',
    'abc',
    'NaN',
    '12abc',
    ' ',
    '1.5',
    '-3',
    '1e3',
    'Infinity',
    '9007199254740993',
  ])('returns null for %p', (raw) => {
    expect(parseDeezerIdParam(raw)).toBeNull();
  });
});

describe('parseStringParam', () => {
  it.each([
    ['a', 'a'],
    [['a', 'b'], 'a'],
  ])('reads %p as %p', (raw, expected) => {
    expect(parseStringParam(raw)).toBe(expected);
  });

  it.each([undefined, '', [], ['']])('returns null for %p', (raw) => {
    expect(parseStringParam(raw)).toBeNull();
  });
});

describe('useTracksFeaturing', () => {
  it('never sends a NaN deezer_id when the route param is malformed', async () => {
    const fa: FeaturedArtist = { name: 'Guest', mbid: null, deezer_id: parseDeezerIdParam('abc') };
    renderHook(() => useTracksFeaturing(fa), { wrapper });

    await waitFor(() => expect(mockListTracksFeaturing).toHaveBeenCalled());
    expect(mockListTracksFeaturing).toHaveBeenCalledWith({
      name: 'Guest',
      mbid: null,
      deezer_id: null,
    });
  });

  it('drops a non-finite deezer_id instead of querying with it', async () => {
    const fa: FeaturedArtist = { name: 'Guest', mbid: null, deezer_id: Number.NaN };
    renderHook(() => useTracksFeaturing(fa), { wrapper });

    await waitFor(() => expect(mockListTracksFeaturing).toHaveBeenCalled());
    expect(mockListTracksFeaturing.mock.calls[0]?.[0].deezer_id).toBeNull();
  });

  it('passes a valid deezer_id through', async () => {
    const fa: FeaturedArtist = { name: 'Guest', mbid: null, deezer_id: parseDeezerIdParam('42') };
    renderHook(() => useTracksFeaturing(fa), { wrapper });

    await waitFor(() => expect(mockListTracksFeaturing).toHaveBeenCalled());
    expect(mockListTracksFeaturing).toHaveBeenCalledWith({
      name: 'Guest',
      mbid: null,
      deezer_id: 42,
    });
  });
});
