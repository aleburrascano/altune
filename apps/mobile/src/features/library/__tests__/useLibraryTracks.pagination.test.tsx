// #792: infinite-scroll pages are fetched by offset. Deleting a track from an earlier,
// already-loaded page renumbers the server list; the next fetchNextPage must follow that
// renumbering instead of trusting the later page's stale offset and skipping a track.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import {
  captureTrackPlacements,
  removeTrackFromCaches,
  restoreTrackPlacements,
} from '@shared/events/trackCachePatch';

import { TRACKS_PAGE_SIZE, useLibraryTracks } from '../hooks/useLibraryTracks';

// A fake server: an ordered list served by offset/limit, exactly like GET /tracks.
let server: TrackResponse[] = [];
const mockGetTracks = jest.fn(({ limit, offset }: { limit: number; offset: number }) =>
  Promise.resolve({
    items: server.slice(offset, offset + limit),
    total: server.length,
    limit,
    offset,
    has_more: offset + limit < server.length,
  }),
);
jest.mock('@shared/api-client/tracks', () => ({
  getTracks: (params: { limit: number; offset: number }) => mockGetTracks(params),
  getAllTracks: jest.fn(),
}));

const track = (i: number) => ({ id: asTrackId(`t${i}`) }) as TrackResponse;

let client: QueryClient;
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

// Two full pages plus a partial third, so page 3 is still unfetched after two loads.
const LIBRARY_SIZE = TRACKS_PAGE_SIZE * 2 + 3;

async function loadTwoPages() {
  const hook = renderHook(() => useLibraryTracks('', 'recent', true), { wrapper });
  await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE));
  act(() => hook.result.current.onEndReached());
  await waitFor(() => expect(hook.result.current.tracks).toHaveLength(TRACKS_PAGE_SIZE * 2));
  return hook;
}

async function loadRest(hook: Awaited<ReturnType<typeof loadTwoPages>>, expected: number) {
  act(() => hook.result.current.onEndReached());
  await waitFor(() => expect(hook.result.current.tracks).toHaveLength(expected));
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  server = Array.from({ length: LIBRARY_SIZE }, (_, i) => track(i));
  mockGetTracks.mockClear();
});

afterEach(() => client.clear());

describe('useLibraryTracks pagination after a delete', () => {
  it('does not skip a track when a track on an earlier loaded page is deleted', async () => {
    const hook = await loadTwoPages();

    server = server.filter((t) => t.id !== 't5');
    act(() => removeTrackFromCaches(client, asTrackId('t5')));
    await loadRest(hook, LIBRARY_SIZE - 1);

    expect(mockGetTracks).toHaveBeenLastCalledWith(
      expect.objectContaining({ offset: TRACKS_PAGE_SIZE * 2 - 1 }),
    );
    expect(hook.result.current.tracks.map((t) => t.id)).toEqual(server.map((t) => t.id));
  });

  it('keeps the offsets right after a failed delete is rolled back', async () => {
    const hook = await loadTwoPages();

    const placements = captureTrackPlacements(client, asTrackId('t5'));
    act(() => removeTrackFromCaches(client, asTrackId('t5')));
    act(() => restoreTrackPlacements(client, placements));
    await loadRest(hook, LIBRARY_SIZE);

    expect(hook.result.current.tracks.map((t) => t.id)).toEqual(server.map((t) => t.id));
  });
});
