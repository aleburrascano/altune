import { QueryClient } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

import { applyServerEvent } from '../applyServerEvent';

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

function seedLibraryHome(qc: QueryClient): void {
  qc.setQueryData<ListTracksResponse>(['library-home'], {
    items: [makeTrack({ id: 'track-1' })],
    total: 1,
    limit: 50,
    offset: 0,
    has_more: false,
  });
}

describe('applyServerEvent', () => {
  it('patches a completed acquisition to ready with audio_ref', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);

    applyServerEvent(qc, {
      id: '1',
      type: 'track_acquisition_completed',
      data: { track_id: 'track-1', audio_ref: 'ref-1' },
    });

    const data = qc.getQueryData<ListTracksResponse>(['library-home']);
    expect(data?.items[0]).toMatchObject({ acquisition_status: 'ready', audio_ref: 'ref-1' });
  });

  it('patches a failed acquisition to failed with reason and clears audio_ref', () => {
    const qc = new QueryClient();
    seedLibraryHome(qc);

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
  });

  it('invalidates list queries for membership events', () => {
    const qc = new QueryClient();
    const spy = jest.spyOn(qc, 'invalidateQueries');

    applyServerEvent(qc, { id: '3', type: 'track_added_to_playlist', data: {} });

    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlist'] });
    expect(spy).toHaveBeenCalledWith({ queryKey: ['playlists'] });
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
  });
});
