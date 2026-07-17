import { QueryClient } from '@tanstack/react-query';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import type {
  ListPlaylistsResponse,
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';

import { applyServerEvent } from '../applyServerEvent';

beforeEach(() => {
  useDownloadStore.getState().reset();
});

function makeTrack(overrides: Partial<TrackResponse>): TrackResponse {
  return {
    id: 'track-1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: 243,
    added_at: '2026-06-30T00:00:00Z',
    acquisition_status: 'pending',
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

function seedLibraryHome(qc: QueryClient, track = makeTrack({ id: 'track-1' })): void {
  qc.setQueryData<ListTracksResponse>(['library-home'], {
    items: [track],
    total: 1,
    limit: 50,
    offset: 0,
    has_more: false,
  });
}

const entries = (): Record<string, unknown> => useDownloadStore.getState().entries;
const phaseOf = (id: string): string | undefined =>
  useDownloadStore.getState().entries[id]?.phase;

describe('applyServerEvent', () => {
  it('seeds the download store and flips the row to pending on a started event', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc, makeTrack({ id: 'track-1', acquisition_status: 'failed', failure_reason: 'x' }));

    applyServerEvent(qc, { id: '0', type: 'track_acquisition_started', data: { track_id: 'track-1' } });

    expect(phaseOf('track-1')).toBe('finding');
    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({ acquisition_status: 'pending', failure_reason: null });
    // Meta is snapshotted from the cache so the dock can render without a fetch.
    expect(useDownloadStore.getState().entries['track-1']).toMatchObject({
      title: 'Midnight City',
      artist: 'M83',
    });
  });

  it('records the phase in the download store on a progress event, without invalidating', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, {
      id: '0',
      type: 'track_acquisition_progress',
      data: { track_id: 'track-1', stage: 'download' },
    });

    expect(phaseOf('track-1')).toBe('downloading');
    expect(spy).not.toHaveBeenCalled();
  });

  it('ignores a progress event with an unknown stage', () => {
    const qc = new QueryClient();
    applyServerEvent(qc, {
      id: '0',
      type: 'track_acquisition_progress',
      data: { track_id: 'track-1', stage: 'bogus' },
    });
    expect(phaseOf('track-1')).toBeUndefined();
  });

  it('patches a completed acquisition to ready and runs the terminal sequence', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);
    useDownloadStore.getState().progress('track-1', 'downloading');

    applyServerEvent(qc, {
      id: '1',
      type: 'track_acquisition_completed',
      data: { track_id: 'track-1', audio_ref: 'ref-1' },
    });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({ acquisition_status: 'ready', audio_ref: 'ref-1' });
    // Not cleared in the same tick — the finishing → done ✓ tail keeps it mounted.
    expect(phaseOf('track-1')).toBe('finishing');
  });

  it('patches a failed acquisition to failed with reason and marks the store failed', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);
    useDownloadStore.getState().progress('track-1', 'downloading');

    applyServerEvent(qc, {
      id: '2',
      type: 'track_acquisition_failed',
      data: { track_id: 'track-1', reason: 'no source found' },
    });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({
      acquisition_status: 'failed',
      failure_reason: 'no source found',
      audio_ref: null,
    });
    expect(phaseOf('track-1')).toBe('failed');
  });

  it('inserts a full track from a track_added_to_library payload without refetching (F10)', () => {
    const qc = new QueryClient();
    qc.setQueryData<ListTracksResponse>(['library-home'], {
      items: [],
      total: 0,
      limit: 50,
      offset: 0,
      has_more: false,
    });
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, {
      id: '9',
      type: 'track_added_to_library',
      data: {
        id: 'track-9',
        title: 'New',
        artist: 'Artist',
        album: null,
        duration_seconds: 100,
        added_at: '2026-07-04T00:00:00Z',
        acquisition_status: 'pending',
        artwork_url: null,
      },
    });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items.map((t) => t.id)).toEqual(['track-9']);
    expect(data?.total).toBe(1);
    expect(spy).not.toHaveBeenCalled();
  });

  it('falls back to invalidate for a thin track_added payload (track_id only)', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, {
      id: '9',
      type: 'track_added_to_library',
      data: { track_id: 'track-9' },
    });

    expect(spy).toHaveBeenCalledWith({ queryKey: ['library-home'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['library', 'featuring'] });
  });

  it('removes the row on track_deleted instead of refetching the library (F11)', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '10', type: 'track_deleted', data: { track_id: 'track-1' } });

    expect(qc.getQueryData<ListTracksResponse>(['library-home'])?.items).toEqual([]);
    expect(spy).not.toHaveBeenCalledWith({ queryKey: ['library-home'] });
    expect(spy).not.toHaveBeenCalledWith({ queryKey: ['library'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlists'] }); // counts reconcile
  });

  it('invalidates list queries for membership events', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '3', type: 'track_added_to_playlist', data: {} });

    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlist'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlists'] });
  });

  it('fully reconciles every SSE-covered family on a resync control event', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '', type: 'resync', data: {} });

    expect(spy).toHaveBeenCalledWith({ queryKey: ['library-home'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['library', 'featuring'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlists'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlist'] });
  });

  it('patches the playlist name on playlist_renamed (F13)', () => {
    const qc = new QueryClient();
    qc.setQueryData<PlaylistDetailResponse>(['playlist', 'p1'], {
      id: 'p1',
      name: 'Old',
      track_count: 0,
      preview_artwork_urls: [],
      created_at: 'x',
      updated_at: 'x',
      tracks: [],
    });
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, {
      id: '1',
      type: 'playlist_renamed',
      data: { playlist_id: 'p1', name: 'New' },
    });

    expect(qc.getQueryData<PlaylistDetailResponse>(['playlist', 'p1'])?.name).toBe('New');
    expect(spy).not.toHaveBeenCalled();
  });

  it('removes a track from the playlist detail on track_removed_from_playlist (F13)', () => {
    const qc = new QueryClient();
    qc.setQueryData<PlaylistDetailResponse>(['playlist', 'p1'], {
      id: 'p1',
      name: 'PL',
      track_count: 2,
      preview_artwork_urls: [],
      created_at: 'x',
      updated_at: 'x',
      tracks: [makeTrack({ id: 'a' }), makeTrack({ id: 'b' })],
    });

    applyServerEvent(qc, {
      id: '2',
      type: 'track_removed_from_playlist',
      data: { playlist_id: 'p1', track_id: 'a' },
    });

    const detail = qc.getQueryData<PlaylistDetailResponse>(['playlist', 'p1']);
    expect(detail?.tracks.map((t) => t.id)).toEqual(['b']);
    expect(detail?.track_count).toBe(1);
  });

  it('reorders the playlist detail tracks on playlist_reordered (F13)', () => {
    const qc = new QueryClient();
    qc.setQueryData<PlaylistDetailResponse>(['playlist', 'p1'], {
      id: 'p1',
      name: 'PL',
      track_count: 3,
      preview_artwork_urls: [],
      created_at: 'x',
      updated_at: 'x',
      tracks: [makeTrack({ id: 'a' }), makeTrack({ id: 'b' }), makeTrack({ id: 'c' })],
    });

    applyServerEvent(qc, {
      id: '3',
      type: 'playlist_reordered',
      data: { playlist_id: 'p1', track_ids: ['c', 'a', 'b'] },
    });

    expect(
      qc.getQueryData<PlaylistDetailResponse>(['playlist', 'p1'])?.tracks.map((t) => t.id),
    ).toEqual(['c', 'a', 'b']);
  });

  it('reconciles playlist list counts alongside a removal (F13)', () => {
    const qc = new QueryClient();
    qc.setQueryData<ListPlaylistsResponse>(['playlists'], {
      items: [
        { id: 'p1', name: 'PL', track_count: 2, preview_artwork_urls: [], created_at: 'x', updated_at: 'x' },
      ],
      total: 1,
    });
    qc.setQueryData<PlaylistDetailResponse>(['playlist', 'p1'], {
      id: 'p1',
      name: 'PL',
      track_count: 2,
      preview_artwork_urls: [],
      created_at: 'x',
      updated_at: 'x',
      tracks: [makeTrack({ id: 'a' })],
    });

    applyServerEvent(qc, {
      id: '4',
      type: 'track_removed_from_playlist',
      data: { playlist_id: 'p1', track_id: 'a' },
    });

    expect(qc.getQueryData<ListPlaylistsResponse>(['playlists'])?.items[0]?.track_count).toBe(1);
  });

  it('ignores unknown event types', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '4', type: 'some_future_event', data: {} });

    expect(spy).not.toHaveBeenCalled();
  });

  it('ignores an acquisition event with no track_id', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);

    applyServerEvent(qc, { id: '5', type: 'track_acquisition_completed', data: {} });

    expect(qc.getQueryData<ListTracksResponse>(['library-home'])?.items[0]?.acquisition_status).toBe(
      'pending',
    );
    expect(entries()).toEqual({});
  });
});
