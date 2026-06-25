import type { TrackResponse } from '@shared/api-client/types';

import { saveControlState } from '../save-control-state';

function _track(status: TrackResponse['acquisition_status']): TrackResponse {
  return {
    id: 'id-1',
    title: 'Midnight City',
    artist: 'M83',
    album: null,
    duration_seconds: 243,
    added_at: '2026-01-01T00:00:00Z',
    acquisition_status: status,
    artwork_url: null,
    failure_reason: status === 'failed' ? 'boom' : null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: status === 'ready' ? 'ref-1' : null,
  };
}

describe('saveControlState', () => {
  it('offers add when the track is not in the library', () => {
    expect(saveControlState(null)).toBe('add');
  });

  it('shows saving while a saved track is still pending acquisition', () => {
    expect(saveControlState(_track('pending'))).toBe('saving');
  });

  it('shows ready once acquisition completes', () => {
    expect(saveControlState(_track('ready'))).toBe('ready');
  });

  it('shows failed when acquisition failed', () => {
    expect(saveControlState(_track('failed'))).toBe('failed');
  });
});
