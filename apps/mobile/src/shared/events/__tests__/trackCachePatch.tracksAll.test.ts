import { QueryClient } from '@tanstack/react-query';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import {
  captureTrackPlacements,
  getTrackFromCaches,
  removeTrackFromCaches,
  upsertTrackInCaches,
} from '../trackCachePatch';

const track = {
  id: asTrackId('t1'),
  title: 'Track One',
  artist: 'Artist One',
  album: null,
  duration_seconds: 180,
  added_at: '2024-01-01T00:00:00Z',
  acquisition_status: 'ready',
} as unknown as TrackResponse;

describe('the loadAll array cache', () => {
  it('lives outside the paged-tracks prefix', () => {
    expect(libraryKeys.tracksAll('', 'recent').slice(0, 2)).not.toEqual(libraryKeys.tracksPrefix);
  });

  it('does not crash the paged-cache patchers', () => {
    const qc = new QueryClient();
    qc.setQueryData(libraryKeys.tracksAll('', 'recent'), [track]);
    expect(() => removeTrackFromCaches(qc, track.id)).not.toThrow();
    expect(() => captureTrackPlacements(qc, track.id)).not.toThrow();
    expect(() => upsertTrackInCaches(qc, track)).not.toThrow();
    expect(() => getTrackFromCaches(qc, track.id)).not.toThrow();
  });
});
