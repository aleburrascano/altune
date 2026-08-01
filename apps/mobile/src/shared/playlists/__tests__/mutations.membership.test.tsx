import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import fc from 'fast-check';

import {
  useAddTracksToPlaylist,
  useDeletePlaylist,
  useRemoveTracksFromPlaylist,
  useRenamePlaylist,
} from '../mutations';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { supabase } from '@shared/auth/supabaseClient';

const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

beforeEach(() => {
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

let alertSpy: jest.SpyInstance;
beforeEach(() => {
  alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
});
afterEach(() => {
  alertSpy.mockRestore();
  jest.useRealTimers();
});

function makePlaylist(overrides: Partial<PlaylistResponse> = {}): PlaylistResponse {
  return {
    id: 'p1',
    name: 'Focus',
    track_count: 5,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  };
}

function makeList(items: PlaylistResponse[]): ListPlaylistsResponse {
  return { items, total: items.length };
}

function makeTrack(overrides: Partial<TrackResponse> = {}): TrackResponse {
  return {
    id: 't1',
    title: 'Track One',
    artist: 'Artist One',
    album: null,
    duration_seconds: 180,
    added_at: '2024-01-01T00:00:00Z',
    acquisition_status: 'ready',
    artwork_url: null,
    failure_reason: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    ...overrides,
  };
}

function makeDetail(
  id: string,
  tracks: TrackResponse[],
  overrides: Partial<PlaylistDetailResponse> = {},
): PlaylistDetailResponse {
  return {
    id,
    name: 'Focus',
    track_count: tracks.length,
    preview_artwork_urls: [],
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    total_duration_seconds: 0,
    tracks,
    ...overrides,
  };
}

function newClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

async function flushMicrotasks(times = 6): Promise<void> {
  for (let i = 0; i < times; i += 1) {
    await Promise.resolve();
  }
}

describe('useAddTracksToPlaylist(): onMutate optimistic bump', () => {
  it('bumps track_count by the requested batch size on the acting playlist mid-flight, and leaves every other playlist byte-identical', async () => {
    jest.useFakeTimers();
    __http.hang('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();
    const target = makePlaylist({ id: 'p1', track_count: 5 });
    const other = makePlaylist({ id: 'p2', name: 'Chill', track_count: 3 });
    queryClient.setQueryData(playlistKeys.list, makeList([target, other]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate({ playlistId: 'p1', trackIds: ['t1', 't2'] });
    });
    await act(async () => {
      await flushMicrotasks();
    });

    const midFlight = queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
    expect(midFlight.items.find((p) => p.id === 'p1')?.track_count).toBe(7);
    expect(midFlight.items.find((p) => p.id === 'p2')).toEqual(other);

    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks();
    });
  });

  it('does not throw and leaves the list cache untouched when nothing is cached at the list key', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 0 },
    });
    const queryClient = newClient();

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1'] });
    });

    expect(queryClient.getQueryData(playlistKeys.list)).toBeUndefined();
  });

  it('does not throw and skips the rollback when the request fails against a cold cache', async () => {
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1'] }),
      ).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.list)).toBeUndefined();
    expect(alertSpy).toHaveBeenCalledWith('Add failed', expect.any(String));
  });
});

describe('useAddTracksToPlaylist(): onSuccess skip-count report', () => {
  it('reports a singular skip using the playlist name looked up from the list cache — a track already in the playlist is a skipped count, never an error', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 1 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', name: 'Focus' })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] }),
      ).resolves.toEqual({ added: 1, skipped: 1 });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', 'One track was already in Focus.');
    expect(alertSpy).not.toHaveBeenCalledWith('Add failed', expect.any(String));
  });

  it('reports a plural skip count using the playlist name', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 2 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', name: 'Focus' })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2', 't3'] });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', '2 tracks were already in Focus.');
  });

  it('falls back to a generic message when the mutated playlist id is not present in the cached list (name lookup miss)', async () => {
    __http.reply('POST /v1/playlists/missing/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 1 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', name: 'Focus' })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'missing', trackIds: ['t1', 't2'] });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', 'One track was already in the playlist.');
  });

  it('falls back to the generic message and settles without throwing when playlistKeys.list was never populated — deep-linking straight into PlaylistDetailScreen', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 1 },
    });
    const queryClient = newClient();

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] }),
      ).resolves.toEqual({ added: 1, skipped: 1 });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', 'One track was already in the playlist.');
  });

  it('does not alert when every requested track was added', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', name: 'Focus' })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('does not alert or throw when the server reports more added than requested (adversarial over-count)', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 9, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', name: 'Focus' })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1'] }),
      ).resolves.toEqual({ added: 9, skipped: 0 });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('treats an empty trackIds batch as a no-op: no track_count bump, no alert', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 0, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', track_count: 5 })]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: [] });
    });

    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(5);
    expect(alertSpy).not.toHaveBeenCalled();
  });
});

describe('useAddTracksToPlaylist(): onError rollback', () => {
  it('restores the exact pre-mutation list cache, leaving every playlist untouched', async () => {
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();
    const seeded = makeList([
      makePlaylist({ id: 'p1', track_count: 5 }),
      makePlaylist({ id: 'p2', name: 'Chill', track_count: 3 }),
    ]);
    queryClient.setQueryData(playlistKeys.list, seeded);

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] }),
      ).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(seeded);
  });

  it.each<[string[], string]>([
    [['t1'], 'Could not add the track to the playlist. Please try again.'],
    [['t1', 't2'], 'Could not add the tracks to the playlist. Please try again.'],
  ])('alerts with the copy the exact batch length turns on (trackIds=%j)', async (trackIds, message) => {
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync({ playlistId: 'p1', trackIds })).rejects.toThrow();
    });

    expect(alertSpy).toHaveBeenCalledWith('Add failed', message);
  });
});

describe('useAddTracksToPlaylist(): onSettled invalidation', () => {
  it('invalidates exactly playlistKeys.list and playlistKeys.detail(playlistId), by identity, on success', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 0 },
    });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1'] });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.detail('p1') });
  });

  it('invalidates the same two keys on failure', async () => {
    __http.fail('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1'] }),
      ).rejects.toThrow();
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.detail('p1') });
  });
});

describe('useAddTracksToPlaylist(): replay is not idempotent, and the list invalidation is what bounds it', () => {
  it('bumps track_count again on a second identical add, re-issuing the list invalidation a real refetch would correct it with', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(playlistKeys.list, makeList([makePlaylist({ id: 'p1', track_count: 5 })]));
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] });
    });
    await act(async () => {
      await result.current.mutateAsync({ playlistId: 'p1', trackIds: ['t1', 't2'] });
    });

    expect(
      queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!.items[0]!.track_count,
    ).toBe(9);
    const listInvalidations = invalidateSpy.mock.calls.filter(
      ([arg]) => (arg as { queryKey: unknown } | undefined)?.queryKey === playlistKeys.list,
    );
    expect(listInvalidations).toHaveLength(2);
  });
});

describe('useAddTracksToPlaylist(): law — an add patch touches only the target playlist', () => {
  it('every other playlist is byte-identical for any cache', async () => {
    const idPool = ['p1', 'p2', 'p3', 'p4', 'p5'];
    const trackPool = ['t1', 't2', 't3', 't4'];
    idPool.forEach((id) => {
      __http.reply(`POST /v1/playlists/${id}/tracks/batch`, {
        status: 200,
        json: { added: 0, skipped: 0 },
      });
    });

    await fc.assert(
      fc.asyncProperty(
        fc.shuffledSubarray(idPool, { minLength: 2 }),
        fc.shuffledSubarray(trackPool, { minLength: 1 }),
        async (ids, trackIds) => {
          const queryClient = newClient();
          const targetId = ids[0]!;
          const playlists = ids.map((id, i) => makePlaylist({ id, name: id, track_count: i }));
          queryClient.setQueryData(playlistKeys.list, makeList(playlists));

          const { result, unmount } = renderHook(() => useAddTracksToPlaylist(), {
            wrapper: createWrapper(queryClient),
          });

          await act(async () => {
            await result.current.mutateAsync({ playlistId: targetId, trackIds });
          });

          const list = queryClient.getQueryData<ListPlaylistsResponse>(playlistKeys.list)!;
          const others = list.items.filter((p) => p.id !== targetId);
          const expectedOthers = playlists.filter((p) => p.id !== targetId);
          expect(others).toEqual(expectedOthers);

          const target = list.items.find((p) => p.id === targetId)!;
          const originalTarget = playlists.find((p) => p.id === targetId)!;
          expect(target.track_count).toBe(originalTarget.track_count + trackIds.length);

          unmount();
        },
      ),
      { numRuns: 25 },
    );
  });
});

describe('useRenamePlaylist(): onMutate optimistic rename', () => {
  it('replaces the name mid-flight while every other field is left untouched', async () => {
    jest.useFakeTimers();
    __http.hang('PATCH /v1/playlists/p1');
    const queryClient = newClient();
    const seeded = makeDetail('p1', [makeTrack({ id: 'a' })], { name: 'Old Name' });
    queryClient.setQueryData(playlistKeys.detail('p1'), seeded);

    const { result } = renderHook(() => useRenamePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate('New Name');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toEqual({
      ...seeded,
      name: 'New Name',
    });

    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks();
    });
  });

  it('does not throw and leaves the detail cache untouched when nothing is cached', async () => {
    __http.reply('PATCH /v1/playlists/p1', {
      status: 200,
      json: makePlaylist({ id: 'p1', name: 'New Name' }),
    });
    const queryClient = newClient();

    const { result } = renderHook(() => useRenamePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('New Name');
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
  });
});

describe('useRenamePlaylist(): onError rollback', () => {
  it('restores the exact pre-mutation detail cache on a failed rename', async () => {
    __http.fail('PATCH /v1/playlists/p1');
    const queryClient = newClient();
    const seeded = makeDetail('p1', [], { name: 'Old Name' });
    queryClient.setQueryData(playlistKeys.detail('p1'), seeded);

    const { result } = renderHook(() => useRenamePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync('New Name')).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toEqual(seeded);
    expect(alertSpy).toHaveBeenCalledWith(
      'Rename failed',
      'Could not rename the playlist. Please try again.',
    );
  });

  it('does not throw and skips the rollback when the request fails against a cold cache', async () => {
    __http.fail('PATCH /v1/playlists/p1');
    const queryClient = newClient();

    const { result } = renderHook(() => useRenamePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync('New Name')).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
  });
});

describe('useRenamePlaylist(): onSettled invalidation', () => {
  it('invalidates exactly playlistKeys.detail(playlistId) and playlistKeys.list, by identity', async () => {
    __http.reply('PATCH /v1/playlists/p1', {
      status: 200,
      json: makePlaylist({ id: 'p1', name: 'New Name' }),
    });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useRenamePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('New Name');
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.detail('p1') });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('useDeletePlaylist(): onSuccess invalidation', () => {
  it('invalidates exactly playlistKeys.list and playlistKeys.detail(playlistId), by identity', async () => {
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useDeletePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync();
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.detail('p1') });
  });
});

describe('useDeletePlaylist(): a failed delete invalidates nothing — onSuccess-only, not onSettled', () => {
  it('leaves the cache exactly as it was and issues no invalidation when the delete request fails', async () => {
    __http.fail('DELETE /v1/playlists/p1');
    const queryClient = newClient();
    const seededList = makeList([makePlaylist({ id: 'p1' })]);
    const seededDetail = makeDetail('p1', []);
    queryClient.setQueryData(playlistKeys.list, seededList);
    queryClient.setQueryData(playlistKeys.detail('p1'), seededDetail);
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useDeletePlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync()).rejects.toThrow();
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(seededList);
    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toEqual(seededDetail);
    expect(alertSpy).toHaveBeenCalledWith(
      'Delete failed',
      'Could not delete the playlist. Please try again.',
    );
  });
});

describe('useRemoveTracksFromPlaylist(): onMutate filter', () => {
  it('filters mid-flight, before the request settles — not only after', async () => {
    jest.useFakeTimers();
    __http.hang('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })];
    queryClient.setQueryData(playlistKeys.detail('p1'), makeDetail('p1', tracks));

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate(['b']);
    });
    await act(async () => {
      await flushMicrotasks();
    });

    const midFlight = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    expect(midFlight.tracks.map((t) => t.id)).toEqual(['a', 'c']);

    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks();
    });
  });

  it.each<[string, string[]]>([
    ['a single-track removal from the row context menu', ['b']],
    ['a bulk selection removal', ['b', 'c']],
  ])('%s filters the detail cache to exactly the surviving tracks, order preserved', async (_label, removeIds) => {
    __http.reply('DELETE /v1/playlists/p1/tracks', {
      status: 200,
      json: { removed: removeIds.length },
    });
    const queryClient = newClient();
    const tracks = [
      makeTrack({ id: 'a' }),
      makeTrack({ id: 'b' }),
      makeTrack({ id: 'c' }),
      makeTrack({ id: 'd' }),
    ];
    queryClient.setQueryData(playlistKeys.detail('p1'), makeDetail('p1', tracks));

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync(removeIds);
    });

    const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
    const expected = tracks.filter((t) => !removeIds.includes(t.id)).map((t) => t.id);
    expect(detail.tracks.map((t) => t.id)).toEqual(expected);

    const request = __http.last();
    expect(request.method).toBe('DELETE');
    expect(request.path).toBe('/v1/playlists/p1/tracks');
    expect(JSON.parse(request.body)).toEqual({ track_ids: removeIds });
  });

  it('does not throw and leaves the detail cache untouched when nothing is cached', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync(['t1']);
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
  });
});

describe('useRemoveTracksFromPlaylist(): onError rollback', () => {
  it('restores the exact pre-mutation detail cache on a failed, destructive removal', async () => {
    __http.fail('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })];
    const seeded = makeDetail('p1', tracks);
    queryClient.setQueryData(playlistKeys.detail('p1'), seeded);

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync(['b'])).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toEqual(seeded);
  });

  it('does not throw and skips the rollback when the request fails against a cold cache', async () => {
    __http.fail('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync(['t1'])).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail('p1'))).toBeUndefined();
    expect(alertSpy).toHaveBeenCalledWith('Remove failed', expect.any(String));
  });

  it.each<[string[], string]>([
    [['t1'], 'Could not remove the track. Please try again.'],
    [['t1', 't2'], 'Could not remove the tracks. Please try again.'],
  ])('alerts with the copy the exact batch length turns on (trackIds=%j)', async (trackIds, message) => {
    __http.fail('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync(trackIds)).rejects.toThrow();
    });

    expect(alertSpy).toHaveBeenCalledWith('Remove failed', message);
  });
});

describe('useRemoveTracksFromPlaylist(): onSettled invalidation', () => {
  it('invalidates exactly playlistKeys.detail(playlistId) and playlistKeys.list, by identity', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync(['t1']);
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.detail('p1') });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('useRemoveTracksFromPlaylist(): idempotent replay', () => {
  it('re-removing the same id is a no-op: apply(apply(e)) equals apply(e)', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();
    const tracks = [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })];
    queryClient.setQueryData(playlistKeys.detail('p1'), makeDetail('p1', tracks));

    const { result } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync(['b']);
    });
    const afterFirst = queryClient.getQueryData(playlistKeys.detail('p1'));

    await act(async () => {
      await result.current.mutateAsync(['b']);
    });
    const afterSecond = queryClient.getQueryData(playlistKeys.detail('p1'));

    expect(afterSecond).toEqual(afterFirst);
  });
});

describe('useRemoveTracksFromPlaylist(): law — the remove patch is the order-preserving set difference', () => {
  it('holds for any track list and any removal subset', async () => {
    const ids = ['a', 'b', 'c', 'd', 'e', 'f'];
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 0 } });

    await fc.assert(
      fc.asyncProperty(fc.shuffledSubarray(ids), async (removeIds) => {
        const queryClient = newClient();
        queryClient.setQueryData(
          playlistKeys.detail('p1'),
          makeDetail('p1', ids.map((id) => makeTrack({ id }))),
        );

        const { result, unmount } = renderHook(() => useRemoveTracksFromPlaylist('p1'), {
          wrapper: createWrapper(queryClient),
        });

        await act(async () => {
          await result.current.mutateAsync(removeIds);
        });

        const detail = queryClient.getQueryData<PlaylistDetailResponse>(playlistKeys.detail('p1'))!;
        const expected = ids.filter((id) => !removeIds.includes(id));
        expect(detail.tracks.map((t) => t.id)).toEqual(expected);

        unmount();
      }),
      { numRuns: 25 },
    );
  });
});
