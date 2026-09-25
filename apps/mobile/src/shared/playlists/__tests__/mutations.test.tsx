import React from 'react';
import { Alert } from 'react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';
import fc from 'fast-check';

import {
  useAddTracksToPlaylist,
  useDeletePlaylist,
  useRemoveTracksFromPlaylist,
  useRenamePlaylist,
  useCreatePlaylist,
  useCreatePlaylistWithTracks,
} from '../mutations';
import { ContractError } from '@shared/errors';
import { asPlaylistId, asTrackId, type TrackId, type PlaylistId } from '@shared/api-client/ids';
import type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { supabase } from '@shared/auth/supabaseClient';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import * as playlistsApi from '@shared/api-client/playlists';

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
    id: asPlaylistId('p1'),
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
    id: asTrackId('t1'),
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
  } as TrackResponse;
}

function makeDetail(
  id: string,
  tracks: TrackResponse[],
  overrides: Partial<PlaylistDetailResponse> = {},
): PlaylistDetailResponse {
  return {
    id: asPlaylistId(id),
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
    const target = makePlaylist({ id: asPlaylistId('p1'), track_count: 5 });
    const other = makePlaylist({ id: asPlaylistId('p2'), name: 'Chill', track_count: 3 });
    queryClient.setQueryData(playlistKeys.list, makeList([target, other]));

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
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
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1')],
      });
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
        result.current.mutateAsync({ playlistId: asPlaylistId('p1'), trackIds: [asTrackId('t1')] }),
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
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({
          playlistId: asPlaylistId('p1'),
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        }),
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
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2'), asTrackId('t3')],
      });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', '2 tracks were already in Focus.');
  });

  it('falls back to a generic message when the mutated playlist id is not present in the cached list (name lookup miss)', async () => {
    __http.reply('POST /v1/playlists/missing/tracks/batch', {
      status: 200,
      json: { added: 1, skipped: 1 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        playlistId: asPlaylistId('missing'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
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
        result.current.mutateAsync({
          playlistId: asPlaylistId('p1'),
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        }),
      ).resolves.toEqual({ added: 1, skipped: 1 });
    });

    expect(alertSpy).toHaveBeenCalledWith('Note', 'One track was already in the playlist.');
  });

  it('treats a batch body missing added as a failed add, rather than reading it as a complete batch', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { skipped: 1 } });
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({
          playlistId: asPlaylistId('p1'),
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        }),
      ).rejects.toBeInstanceOf(ContractError);
    });

    expect(alertSpy).toHaveBeenCalledWith('Add failed', expect.any(String));
  });

  it('does not alert when every requested track was added', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });

    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('does not alert or throw when the server reports more added than requested (adversarial over-count)', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 9, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), name: 'Focus' })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: asPlaylistId('p1'), trackIds: [asTrackId('t1')] }),
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
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), track_count: 5 })]),
    );

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({ playlistId: asPlaylistId('p1'), trackIds: [] });
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
      makePlaylist({ id: asPlaylistId('p1'), track_count: 5 }),
      makePlaylist({ id: asPlaylistId('p2'), name: 'Chill', track_count: 3 }),
    ]);
    queryClient.setQueryData(playlistKeys.list, seeded);

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({
          playlistId: asPlaylistId('p1'),
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        }),
      ).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(seeded);
  });

  it('keeps an SSE-delivered list that landed mid-flight instead of restoring the stale snapshot', async () => {
    jest.useFakeTimers();
    __http.hang('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([
        makePlaylist({ id: asPlaylistId('p1'), track_count: 5 }),
        makePlaylist({ id: asPlaylistId('p2'), name: 'Chill', track_count: 3 }),
      ]),
    );
    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate({ playlistId: asPlaylistId('p1'), trackIds: [asTrackId('t1')] });
    });
    await act(async () => {
      await flushMicrotasks();
    });
    const sseRefetched = makeList([
      makePlaylist({ id: asPlaylistId('p1'), track_count: 8 }),
      makePlaylist({ id: asPlaylistId('p2'), name: 'Chill Renamed', track_count: 4 }),
    ]);
    queryClient.setQueryData(playlistKeys.list, sseRefetched);
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(sseRefetched);
  });

  it('leaves the list as it is on failure when the target playlist was not in the snapshot', async () => {
    __http.fail('POST /v1/playlists/p9/tracks/batch');
    const queryClient = newClient();
    const seeded = makeList([makePlaylist({ id: asPlaylistId('p1'), track_count: 5 })]);
    queryClient.setQueryData(playlistKeys.list, seeded);
    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ playlistId: asPlaylistId('p9'), trackIds: [asTrackId('t1')] }),
      ).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(seeded);
  });

  it('reverts only its own bump when an SSE patch touched another playlist mid-flight', async () => {
    jest.useFakeTimers();
    __http.hang('POST /v1/playlists/p1/tracks/batch');
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([
        makePlaylist({ id: asPlaylistId('p1'), track_count: 5 }),
        makePlaylist({ id: asPlaylistId('p2'), name: 'Chill', track_count: 3 }),
      ]),
    );
    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });
    await act(async () => {
      await flushMicrotasks();
    });
    queryClient.setQueryData<ListPlaylistsResponse>(playlistKeys.list, (prev) => ({
      ...prev!,
      items: prev!.items.map((p) => (p.id === 'p2' ? { ...p, name: 'Chill Renamed' } : p)),
    }));
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(
      makeList([
        makePlaylist({ id: asPlaylistId('p1'), track_count: 5 }),
        makePlaylist({ id: asPlaylistId('p2'), name: 'Chill Renamed', track_count: 3 }),
      ]),
    );
  });

  it.each<[TrackId[], string]>([
    [[asTrackId('t1')], 'Could not add the track to the playlist. Please try again.'],
    [
      [asTrackId('t1'), asTrackId('t2')],
      'Could not add the tracks to the playlist. Please try again.',
    ],
  ])(
    'alerts with the copy the exact batch length turns on (trackIds=%j)',
    async (trackIds, message) => {
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      const queryClient = newClient();

      const { result } = renderHook(() => useAddTracksToPlaylist(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await expect(
          result.current.mutateAsync({ playlistId: asPlaylistId('p1'), trackIds }),
        ).rejects.toThrow();
      });

      expect(alertSpy).toHaveBeenCalledWith('Add failed', message);
    },
  );
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
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1')],
      });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: playlistKeys.detail(asPlaylistId('p1')),
    });
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
        result.current.mutateAsync({ playlistId: asPlaylistId('p1'), trackIds: [asTrackId('t1')] }),
      ).rejects.toThrow();
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: playlistKeys.detail(asPlaylistId('p1')),
    });
  });
});

describe('useAddTracksToPlaylist(): replay is not idempotent, and the list invalidation is what bounds it', () => {
  it('bumps track_count again on a second identical add, re-issuing the list invalidation a real refetch would correct it with', async () => {
    __http.reply('POST /v1/playlists/p1/tracks/batch', {
      status: 200,
      json: { added: 2, skipped: 0 },
    });
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.list,
      makeList([makePlaylist({ id: asPlaylistId('p1'), track_count: 5 })]),
    );
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useAddTracksToPlaylist(), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
    });
    await act(async () => {
      await result.current.mutateAsync({
        playlistId: asPlaylistId('p1'),
        trackIds: [asTrackId('t1'), asTrackId('t2')],
      });
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
    const idPool = ['p1', 'p2', 'p3', 'p4', 'p5'].map(asPlaylistId);
    const trackPool = ['t1', 't2', 't3', 't4'].map(asTrackId);
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
    const seeded = makeDetail('p1', [makeTrack({ id: asTrackId('a') })], { name: 'Old Name' });
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), seeded);

    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate('New Name');
    });
    await act(async () => {
      await flushMicrotasks();
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual({
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
      json: makePlaylist({ id: asPlaylistId('p1'), name: 'New Name' }),
    });
    const queryClient = newClient();

    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('New Name');
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
  });
});

describe('useRenamePlaylist(): onError rollback', () => {
  it('restores the exact pre-mutation detail cache on a failed rename', async () => {
    __http.fail('PATCH /v1/playlists/p1');
    const queryClient = newClient();
    const seeded = makeDetail('p1', [], { name: 'Old Name' });
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), seeded);

    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync('New Name')).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(seeded);
    expect(alertSpy).toHaveBeenCalledWith(
      'Rename failed',
      'Could not rename the playlist. Please try again.',
    );
  });

  it('restores only the name, keeping tracks an SSE update added mid-flight', async () => {
    jest.useFakeTimers();
    __http.hang('PATCH /v1/playlists/p1');
    const queryClient = newClient();
    const a = makeTrack({ id: asTrackId('a') });
    const b = makeTrack({ id: asTrackId('b') });
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makeDetail('p1', [a], { name: 'Old' }),
    );
    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate('New');
    });
    await act(async () => {
      await flushMicrotasks();
    });
    queryClient.setQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
      (prev) => ({
        ...prev!,
        tracks: [a, b],
        track_count: 2,
      }),
    );
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(
      makeDetail('p1', [a, b], { name: 'Old' }),
    );
  });

  it('keeps a name an SSE rename delivered mid-flight rather than the stale pre-mutation name', async () => {
    jest.useFakeTimers();
    __http.hang('PATCH /v1/playlists/p1');
    const queryClient = newClient();
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makeDetail('p1', [], { name: 'Old' }),
    );
    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate('New');
    });
    await act(async () => {
      await flushMicrotasks();
    });
    queryClient.setQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
      (prev) => ({
        ...prev!,
        name: 'From Another Device',
      }),
    );
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(
      makeDetail('p1', [], { name: 'From Another Device' }),
    );
  });

  it('does not throw and skips the rollback when the request fails against a cold cache', async () => {
    __http.fail('PATCH /v1/playlists/p1');
    const queryClient = newClient();

    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync('New Name')).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
  });
});

describe('useRenamePlaylist(): onSettled invalidation', () => {
  it('invalidates exactly playlistKeys.detail(playlistId) and playlistKeys.list, by identity', async () => {
    __http.reply('PATCH /v1/playlists/p1', {
      status: 200,
      json: makePlaylist({ id: asPlaylistId('p1'), name: 'New Name' }),
    });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useRenamePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync('New Name');
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: playlistKeys.detail(asPlaylistId('p1')),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('useDeletePlaylist(): onSuccess invalidation', () => {
  it('invalidates exactly playlistKeys.list and playlistKeys.detail(playlistId), by identity', async () => {
    __http.reply('DELETE /v1/playlists/p1', { status: 204 });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync();
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: playlistKeys.detail(asPlaylistId('p1')),
    });
  });
});

describe('useDeletePlaylist(): a failed delete invalidates nothing — onSuccess-only, not onSettled', () => {
  it('leaves the cache exactly as it was and issues no invalidation when the delete request fails', async () => {
    __http.fail('DELETE /v1/playlists/p1');
    const queryClient = newClient();
    const seededList = makeList([makePlaylist({ id: asPlaylistId('p1') })]);
    const seededDetail = makeDetail('p1', []);
    queryClient.setQueryData(playlistKeys.list, seededList);
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), seededDetail);
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync()).rejects.toThrow();
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(playlistKeys.list)).toEqual(seededList);
    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(seededDetail);
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
    const tracks = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makeDetail('p1', tracks));

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate([asTrackId('b')]);
    });
    await act(async () => {
      await flushMicrotasks();
    });

    const midFlight = queryClient.getQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
    )!;
    expect(midFlight.tracks.map((t) => t.id)).toEqual(['a', 'c']);

    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks();
    });
  });

  it.each<[string, TrackId[]]>([
    ['a single-track removal from the row context menu', [asTrackId('b')]],
    ['a bulk selection removal', [asTrackId('b'), asTrackId('c')]],
  ])(
    '%s filters the detail cache to exactly the surviving tracks, order preserved',
    async (_label, removeIds) => {
      __http.reply('DELETE /v1/playlists/p1/tracks', {
        status: 200,
        json: { removed: removeIds.length },
      });
      const queryClient = newClient();
      const tracks = [
        makeTrack({ id: asTrackId('a') }),
        makeTrack({ id: asTrackId('b') }),
        makeTrack({ id: asTrackId('c') }),
        makeTrack({ id: asTrackId('d') }),
      ];
      queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makeDetail('p1', tracks));

      const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync(removeIds);
      });

      const detail = queryClient.getQueryData<PlaylistDetailResponse>(
        playlistKeys.detail(asPlaylistId('p1')),
      )!;
      const expected = tracks.filter((t) => !removeIds.includes(t.id)).map((t) => t.id);
      expect(detail.tracks.map((t) => t.id)).toEqual(expected);

      const request = __http.last();
      expect(request.method).toBe('DELETE');
      expect(request.path).toBe('/v1/playlists/p1/tracks');
      expect(JSON.parse(request.body)).toEqual({ track_ids: removeIds });
    },
  );

  it('does not throw and leaves the detail cache untouched when nothing is cached', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync([asTrackId('t1')]);
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
  });
});

describe('useRemoveTracksFromPlaylist(): onError rollback', () => {
  it('restores the exact pre-mutation detail cache on a failed, destructive removal', async () => {
    __http.fail('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();
    const tracks = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    const seeded = makeDetail('p1', tracks);
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), seeded);

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync([asTrackId('b')])).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(seeded);
  });

  it('does not duplicate a removed track that a mid-flight refetch already restored', async () => {
    jest.useFakeTimers();
    __http.hang('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();
    const [a, b, c] = ['a', 'b', 'c'].map((id) => makeTrack({ id: asTrackId(id) }));
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makeDetail('p1', [a!, b!, c!]),
    );
    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate([asTrackId('a'), asTrackId('c')]);
    });
    await act(async () => {
      await flushMicrotasks();
    });
    const refetched = makeDetail('p1', [a!, b!], { name: 'Refetched' });
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), refetched);
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(
      makeDetail('p1', [a!, b!, c!], { name: 'Refetched', track_count: 2 }),
    );
  });

  it('re-inserts the removed tracks in place while keeping a track an SSE update added mid-flight', async () => {
    jest.useFakeTimers();
    __http.hang('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();
    const [a, b, c, d] = ['a', 'b', 'c', 'd'].map((id) => makeTrack({ id: asTrackId(id) }));
    queryClient.setQueryData(
      playlistKeys.detail(asPlaylistId('p1')),
      makeDetail('p1', [a!, b!, c!]),
    );
    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    act(() => {
      result.current.mutate([asTrackId('b')]);
    });
    await act(async () => {
      await flushMicrotasks();
    });
    queryClient.setQueryData<PlaylistDetailResponse>(
      playlistKeys.detail(asPlaylistId('p1')),
      (prev) => ({
        ...prev!,
        name: 'Focus Renamed',
        tracks: [...prev!.tracks, d!],
      }),
    );
    await act(async () => {
      jest.advanceTimersByTime(15_000);
      await flushMicrotasks(20);
    });

    expect(result.current.isError).toBe(true);
    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toEqual(
      makeDetail('p1', [a!, b!, c!, d!], { name: 'Focus Renamed', track_count: 3 }),
    );
  });

  it('does not throw and skips the rollback when the request fails against a cold cache', async () => {
    __http.fail('DELETE /v1/playlists/p1/tracks');
    const queryClient = newClient();

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await expect(result.current.mutateAsync([asTrackId('t1')])).rejects.toThrow();
    });

    expect(queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')))).toBeUndefined();
    expect(alertSpy).toHaveBeenCalledWith('Remove failed', expect.any(String));
  });

  it.each<[TrackId[], string]>([
    [[asTrackId('t1')], 'Could not remove the track. Please try again.'],
    [[asTrackId('t1'), asTrackId('t2')], 'Could not remove the tracks. Please try again.'],
  ])(
    'alerts with the copy the exact batch length turns on (trackIds=%j)',
    async (trackIds, message) => {
      __http.fail('DELETE /v1/playlists/p1/tracks');
      const queryClient = newClient();

      const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await expect(result.current.mutateAsync(trackIds)).rejects.toThrow();
      });

      expect(alertSpy).toHaveBeenCalledWith('Remove failed', message);
    },
  );
});

describe('useRemoveTracksFromPlaylist(): onSettled invalidation', () => {
  it('invalidates exactly playlistKeys.detail(playlistId) and playlistKeys.list, by identity', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync([asTrackId('t1')]);
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: playlistKeys.detail(asPlaylistId('p1')),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
  });
});

describe('useRemoveTracksFromPlaylist(): idempotent replay', () => {
  it('re-removing the same id is a no-op: apply(apply(e)) equals apply(e)', async () => {
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 1 } });
    const queryClient = newClient();
    const tracks = [
      makeTrack({ id: asTrackId('a') }),
      makeTrack({ id: asTrackId('b') }),
      makeTrack({ id: asTrackId('c') }),
    ];
    queryClient.setQueryData(playlistKeys.detail(asPlaylistId('p1')), makeDetail('p1', tracks));

    const { result } = renderHook(() => useRemoveTracksFromPlaylist(asPlaylistId('p1')), {
      wrapper: createWrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync([asTrackId('b')]);
    });
    const afterFirst = queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')));

    await act(async () => {
      await result.current.mutateAsync([asTrackId('b')]);
    });
    const afterSecond = queryClient.getQueryData(playlistKeys.detail(asPlaylistId('p1')));

    expect(afterSecond).toEqual(afterFirst);
  });
});

describe('useRemoveTracksFromPlaylist(): law — the remove patch is the order-preserving set difference', () => {
  it('holds for any track list and any removal subset', async () => {
    const ids = ['a', 'b', 'c', 'd', 'e', 'f'].map(asTrackId);
    __http.reply('DELETE /v1/playlists/p1/tracks', { status: 200, json: { removed: 0 } });

    await fc.assert(
      fc.asyncProperty(fc.shuffledSubarray(ids), async (removeIds) => {
        const queryClient = newClient();
        queryClient.setQueryData(
          playlistKeys.detail(asPlaylistId('p1')),
          makeDetail(
            'p1',
            ids.map((id) => makeTrack({ id })),
          ),
        );

        const { result, unmount } = renderHook(
          () => useRemoveTracksFromPlaylist(asPlaylistId('p1')),
          {
            wrapper: createWrapper(queryClient),
          },
        );

        await act(async () => {
          await result.current.mutateAsync(removeIds);
        });

        const detail = queryClient.getQueryData<PlaylistDetailResponse>(
          playlistKeys.detail(asPlaylistId('p1')),
        )!;
        const expected = ids.filter((id) => !removeIds.includes(id));
        expect(detail.tracks.map((t) => t.id)).toEqual(expected);

        unmount();
      }),
      { numRuns: 25 },
    );
  });
});

describe('create', () => {
  // A full PlaylistResponse: the api-client now contract-parses the create body,
  // so a thin { id, name } payload would be rejected before the hook sees it.
  function created(id: string, name: string) {
    return {
      id,
      name,
      track_count: 0,
      preview_artwork_urls: [],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    };
  }

  function freshClient() {
    return new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
  }

  let alertSpy: jest.SpyInstance;

  beforeEach(() => {
    (supabase.auth.getSession as jest.Mock).mockResolvedValue({
      data: { session: { access_token: 'tok' } },
      error: null,
    });
    alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
  });

  afterEach(() => {
    alertSpy.mockRestore();
  });

  describe('useCreatePlaylistWithTracks: addTracksToPlaylist fails after createPlaylist already landed (:36-43)', () => {
    it('a dropped connection between the two awaited requests deletes the playlist the create already landed, leaving no empty orphan behind', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.reply('DELETE /v1/playlists/p1', { status: 204 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      });

      expect(__http.countFor('DELETE /v1/playlists/p1')).toBe(1);
    });

    it('a rolled-back create resolves the mutation without a dangling playlist, so no caller can act on one that no longer exists', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.reply('DELETE /v1/playlists/p1', { status: 204 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
      await act(async () => {
        data = await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      });

      expect(result.current.isError).toBe(false);
      expect(data).toEqual({ added: 0, addFailed: true });
    });

    it('a transient 5xx from the batch-add endpoint is compensated the same way, not rethrown as a mutation error', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p2', 'Chill') });
      __http.reply('POST /v1/playlists/p2/tracks/batch', { status: 503 });
      __http.reply('DELETE /v1/playlists/p2', { status: 204 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
      await act(async () => {
        data = await result.current.mutateAsync({
          name: 'Chill',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(result.current.isError).toBe(false);
      expect(data.addFailed).toBe(true);
      expect(data.playlist).toBeUndefined();
      expect(__http.countFor('DELETE /v1/playlists/p2')).toBe(1);
    });

    it('an add that lands never triggers the compensating delete', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.reply('POST /v1/playlists/p1/tracks/batch', {
        status: 200,
        json: { added: 1, skipped: 0 },
      });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      });

      expect(__http.countFor('DELETE /v1/playlists/p1')).toBe(0);
    });

    it('the compensating delete failing too keeps the created playlist in the resolved result, the only case a caller is handed one', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.fail('DELETE /v1/playlists/p1');
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      let data!: Awaited<ReturnType<typeof result.current.mutateAsync>>;
      await act(async () => {
        data = await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      });

      expect(result.current.isError).toBe(false);
      expect(data).toEqual({ playlist: created('p1', 'Focus'), added: 0, addFailed: true });
    });
  });

  describe('useCreatePlaylistWithTracks: onSuccess note after a failed add (:45-54)', () => {
    it('a rolled-back create tells the user nothing was created, never that a playlist is waiting for manual adds', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.reply('DELETE /v1/playlists/p1', { status: 204 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Focus',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(alertSpy).toHaveBeenCalledTimes(1);
      expect(alertSpy).toHaveBeenCalledWith(
        'Error',
        'Could not create the playlist. Please try again.',
      );
    });

    it.each([
      [
        'a single requested track',
        [asTrackId('t1')],
        'Playlist created, but the track could not be added. Try adding it manually.',
      ],
      [
        'more than one requested track',
        [asTrackId('t1'), asTrackId('t2')],
        'Playlist created, but the tracks could not be added. Try adding them manually.',
      ],
    ] as const)(
      '%s -> a surviving orphan keeps the manual-add copy, singular/plural on trackIds.length === 1',
      async (_label, trackIds, expectedMessage) => {
        __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
        __http.fail('POST /v1/playlists/p1/tracks/batch');
        __http.fail('DELETE /v1/playlists/p1');
        const queryClient = freshClient();
        const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
          wrapper: createWrapper(queryClient),
        });

        await act(async () => {
          await result.current.mutateAsync({ name: 'Focus', trackIds: [...trackIds] });
        });

        expect(alertSpy).toHaveBeenCalledWith('Note', expectedMessage);
      },
    );

    it('a surviving orphan suppresses the skip-count alert even though added(0) < trackIds.length', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.fail('DELETE /v1/playlists/p1');
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Focus',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(alertSpy).toHaveBeenCalledTimes(1);
    });
  });

  describe('useCreatePlaylistWithTracks: onSuccess skip-count note on a brand-new playlist (:55-57), alreadyThereMessage table', () => {
    it.each([
      [
        'a duplicate track id inside one request, playlist name known from the create response',
        [asTrackId('t1'), asTrackId('t1')],
        { added: 1, skipped: 1 },
        'Focus',
        'One track was already in Focus.',
      ],
      [
        'a track deleted between building the selection and the request landing, playlist name known',
        [asTrackId('t1'), asTrackId('t2'), asTrackId('t3')],
        { added: 1, skipped: 2 },
        'Focus',
        '2 tracks were already in Focus.',
      ],
    ] as const)('%s', async (_label, trackIds, addResponse, playlistName, expectedMessage) => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', playlistName) });
      __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: addResponse });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({ name: 'irrelevant', trackIds: [...trackIds] });
      });

      expect(result.current.isError).toBe(false);
      expect(alertSpy).toHaveBeenCalledWith('Note', expectedMessage);
    });

    it('a thin create response missing name is rejected by the contract layer: a mutation error, never a batch-add', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: { id: 'p1' } });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      let caught: unknown;
      await act(async () => {
        try {
          await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
        } catch (error) {
          caught = error;
        }
      });

      expect(caught).toBeInstanceOf(ContractError);
      expect(__http.countFor('POST /v1/playlists/p1/tracks/batch')).toBe(0);
      expect(alertSpy).toHaveBeenCalledWith(
        'Error',
        'Could not create the playlist. Please try again.',
      );
    });

    it('every requested track lands (added === trackIds.length) stays silent, no false skip note', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.reply('POST /v1/playlists/p1/tracks/batch', {
        status: 200,
        json: { added: 2, skipped: 0 },
      });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Focus',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(alertSpy).not.toHaveBeenCalled();
    });

    it('a batch body missing added is refused by the contract layer, so the skip note can never read "NaN tracks"', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.reply('POST /v1/playlists/p1/tracks/batch', { status: 200, json: { skipped: 0 } });
      __http.reply('DELETE /v1/playlists/p1', { status: 204 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Focus',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(alertSpy).not.toHaveBeenCalledWith('Note', expect.stringContaining('NaN'));
      expect(alertSpy).toHaveBeenCalledWith(
        'Error',
        'Could not create the playlist. Please try again.',
      );
    });

    it('a malformed server body claiming more added than requested does not produce a lying skip-count note', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.reply('POST /v1/playlists/p1/tracks/batch', {
        status: 200,
        json: { added: 5, skipped: 0 },
      });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Focus',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      expect(alertSpy).not.toHaveBeenCalled();
    });
  });

  describe('createPlaylist itself failing (:26-28, :59-61)', () => {
    it('useCreatePlaylist surfaces a server 500 as a mutation error and alerts the user, without creating anything', async () => {
      __http.reply('POST /v1/playlists', { status: 500 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylist(), {
        wrapper: createWrapper(queryClient),
      });

      let caught: unknown;
      await act(async () => {
        try {
          await result.current.mutateAsync('Focus');
        } catch (error) {
          caught = error;
        }
      });

      expect(caught).toBeDefined();
      await waitFor(() => expect(result.current.isError).toBe(true));
      expect(alertSpy).toHaveBeenCalledWith(
        'Error',
        'Could not create the playlist. Please try again.',
      );
    });

    it('useCreatePlaylistWithTracks surfaces the same failure, and never reaches the batch-add endpoint', async () => {
      __http.reply('POST /v1/playlists', { status: 500 });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      let caught: unknown;
      await act(async () => {
        try {
          await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
        } catch (error) {
          caught = error;
        }
      });

      expect(caught).toBeDefined();
      await waitFor(() => expect(result.current.isError).toBe(true));
      expect(alertSpy).toHaveBeenCalledWith(
        'Error',
        'Could not create the playlist. Please try again.',
      );
      expect(
        __http.requests.filter((r: { path: string }) => r.path.includes('tracks/batch')),
      ).toHaveLength(0);
    });
  });

  describe('onSettled invalidation (:29, :62) — exact key identity', () => {
    it('useCreatePlaylist invalidates playlistKeys.list, and only that key, on success', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      const queryClient = freshClient();
      const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
      const { result } = renderHook(() => useCreatePlaylist(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync('Focus');
      });

      expect(invalidateSpy).toHaveBeenCalledTimes(1);
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    });

    it('useCreatePlaylist invalidates playlistKeys.list even when the create call fails', async () => {
      __http.reply('POST /v1/playlists', { status: 500 });
      const queryClient = freshClient();
      const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
      const { result } = renderHook(() => useCreatePlaylist(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync('Focus').catch(() => {});
      });

      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    });

    it('useCreatePlaylistWithTracks invalidates playlistKeys.list, and only that key, even when the add step failed and the playlist was rolled back', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Focus') });
      __http.fail('POST /v1/playlists/p1/tracks/batch');
      __http.reply('DELETE /v1/playlists/p1', { status: 204 });
      const queryClient = freshClient();
      const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({ name: 'Focus', trackIds: [asTrackId('t1')] });
      });

      expect(invalidateSpy).toHaveBeenCalledTimes(1);
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: playlistKeys.list });
    });
  });

  describe('request bodies sent over the wire', () => {
    it('useCreatePlaylist POSTs the exact { name } body, not an empty one', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Road Trip Mix') });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylist(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync('Road Trip Mix');
      });

      const request = __http.requests.find(
        (r: { method: string; path: string }) => r.method === 'POST' && r.path === '/v1/playlists',
      );
      expect(JSON.parse(request.body)).toEqual({ name: 'Road Trip Mix' });
    });

    it('useCreatePlaylistWithTracks POSTs the exact { name } create body and the exact { track_ids } add body, neither an empty one', async () => {
      __http.reply('POST /v1/playlists', { status: 201, json: created('p1', 'Road Trip Mix') });
      __http.reply('POST /v1/playlists/p1/tracks/batch', {
        status: 200,
        json: { added: 2, skipped: 0 },
      });
      const queryClient = freshClient();
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), {
        wrapper: createWrapper(queryClient),
      });

      await act(async () => {
        await result.current.mutateAsync({
          name: 'Road Trip Mix',
          trackIds: [asTrackId('t1'), asTrackId('t2')],
        });
      });

      const createRequest = __http.requests.find(
        (r: { method: string; path: string }) => r.method === 'POST' && r.path === '/v1/playlists',
      );
      const addRequest = __http.requests.find(
        (r: { method: string; path: string }) =>
          r.method === 'POST' && r.path === '/v1/playlists/p1/tracks/batch',
      );
      expect(JSON.parse(createRequest.body)).toEqual({ name: 'Road Trip Mix' });
      expect(JSON.parse(addRequest.body)).toEqual({ track_ids: ['t1', 't2'] });
    });
  });
});

describe('after sign-out', () => {
  const mockCreatePlaylist = jest.fn<Promise<PlaylistResponse>, [{ name: string }]>();
  const mockAddTracks = jest.fn<Promise<{ added: number }>, [PlaylistId, unknown]>();
  const mockDeletePlaylist = jest.fn<Promise<void>, [PlaylistId]>();

  type Deferred<T> = { promise: Promise<T>; resolve: (v: T) => void; reject: (e: Error) => void };

  function deferred<T>(): Deferred<T> {
    let resolve!: (v: T) => void;
    let reject!: (e: Error) => void;
    const promise = new Promise<T>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  }

  function playlist(id: string, name: string, trackCount = 0): PlaylistResponse {
    return {
      id: asPlaylistId(id),
      name,
      track_count: trackCount,
      preview_artwork_urls: [],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    } as PlaylistResponse;
  }

  const USER_B_PLAYLISTS = { items: [playlist('b1', 'B Mix', 3)] };

  function setup() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    }
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    return { queryClient, wrapper: Wrapper, invalidateSpy };
  }

  function signOutThenLoadUserB(queryClient: QueryClient): void {
    runSignOutCleanups();
    queryClient.clear();
    queryClient.setQueryData(playlistKeys.list, USER_B_PLAYLISTS);
  }

  let alertSpy: jest.SpyInstance;

  beforeEach(() => {
    jest
      .spyOn(playlistsApi, 'createPlaylist')
      .mockImplementation((body: { name: string }) => mockCreatePlaylist(body));
    jest
      .spyOn(playlistsApi, 'addTracksToPlaylist')
      .mockImplementation((id: PlaylistId, body: unknown) => mockAddTracks(id, body) as never);
    jest
      .spyOn(playlistsApi, 'deletePlaylist')
      .mockImplementation((id: PlaylistId) => mockDeletePlaylist(id));
    jest.spyOn(playlistsApi, 'removeTracksFromPlaylist').mockImplementation(jest.fn());
    jest.spyOn(playlistsApi, 'renamePlaylist').mockImplementation(jest.fn());
    mockCreatePlaylist.mockReset();
    mockAddTracks.mockReset();
    mockDeletePlaylist.mockReset();
    alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => undefined);
  });

  afterEach(() => {
    alertSpy.mockRestore();
    jest.restoreAllMocks();
  });

  // Regression test for #2729.
  describe('playlist mutations that settle after sign-out leave the next user untouched', () => {
    it('useCreatePlaylist: a create failing after sign-out neither alerts nor refetches user B playlists', async () => {
      const { queryClient, wrapper, invalidateSpy } = setup();
      const request = deferred<PlaylistResponse>();
      mockCreatePlaylist.mockReturnValue(request.promise);
      const { result } = renderHook(() => useCreatePlaylist(), { wrapper });
      act(() => result.current.mutate('A Mix'));
      await waitFor(() => expect(mockCreatePlaylist).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => request.reject(new Error('offline')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(alertSpy).not.toHaveBeenCalled();
      expect(invalidateSpy).not.toHaveBeenCalled();
      expect(queryClient.getQueryData(playlistKeys.list)).toEqual(USER_B_PLAYLISTS);
    });

    it("useCreatePlaylistWithTracks: a create landing after sign-out never sends the add under the next user's session", async () => {
      const { queryClient, wrapper } = setup();
      const create = deferred<PlaylistResponse>();
      mockCreatePlaylist.mockReturnValue(create.promise);
      mockAddTracks.mockResolvedValue({ added: 1 });
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper });
      act(() => result.current.mutate({ name: 'A Mix', trackIds: [asTrackId('a1')] }));
      await waitFor(() => expect(mockCreatePlaylist).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => create.resolve(playlist('a-new', 'A Mix')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(mockAddTracks).not.toHaveBeenCalled();
      expect(mockDeletePlaylist).not.toHaveBeenCalled();
      expect(alertSpy).not.toHaveBeenCalled();
    });

    it("useCreatePlaylistWithTracks: an add failing after sign-out does not roll back under the next user's session", async () => {
      const { queryClient, wrapper } = setup();
      mockCreatePlaylist.mockResolvedValue(playlist('a-new', 'A Mix'));
      const add = deferred<{ added: number }>();
      mockAddTracks.mockReturnValue(add.promise);
      const { result } = renderHook(() => useCreatePlaylistWithTracks(), { wrapper });
      act(() => result.current.mutate({ name: 'A Mix', trackIds: [asTrackId('a1')] }));
      await waitFor(() => expect(mockAddTracks).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => add.reject(new Error('offline')));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(mockDeletePlaylist).not.toHaveBeenCalled();
      expect(alertSpy).not.toHaveBeenCalled();
    });

    it('useDeletePlaylist: a delete failing after sign-out does not alert the next user', async () => {
      const { queryClient, wrapper } = setup();
      const request = deferred<void>();
      mockDeletePlaylist.mockReturnValue(request.promise);
      const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('a1')), { wrapper });
      act(() => result.current.mutate());
      await waitFor(() => expect(mockDeletePlaylist).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => request.reject(new Error('offline')));
      await waitFor(() => expect(result.current.isError).toBe(true));

      expect(alertSpy).not.toHaveBeenCalled();
    });

    it('useDeletePlaylist: a delete succeeding after sign-out does not refetch user B playlists', async () => {
      const { queryClient, wrapper, invalidateSpy } = setup();
      const request = deferred<void>();
      mockDeletePlaylist.mockReturnValue(request.promise);
      const { result } = renderHook(() => useDeletePlaylist(asPlaylistId('a1')), { wrapper });
      act(() => result.current.mutate());
      await waitFor(() => expect(mockDeletePlaylist).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => request.resolve());
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(invalidateSpy).not.toHaveBeenCalled();
    });

    it("useAddTracksToPlaylist: an add answering after sign-out does not tell user B about user A's playlist", async () => {
      const { queryClient, wrapper } = setup();
      queryClient.setQueryData(playlistKeys.list, { items: [playlist('a1', 'A Mix', 2)] });
      const request = deferred<{ added: number }>();
      mockAddTracks.mockReturnValue(request.promise);
      const { result } = renderHook(() => useAddTracksToPlaylist(), { wrapper });
      const trackIds = [asTrackId('t1'), asTrackId('t2')];
      act(() => result.current.mutate({ playlistId: asPlaylistId('a1'), trackIds }));
      await waitFor(() => expect(mockAddTracks).toHaveBeenCalled());

      signOutThenLoadUserB(queryClient);
      await act(async () => request.resolve({ added: 0 }));
      await waitFor(() => expect(result.current.isSuccess).toBe(true));

      expect(alertSpy).not.toHaveBeenCalled();
      expect(queryClient.getQueryData(playlistKeys.list)).toEqual(USER_B_PLAYLISTS);
    });
  });
});
