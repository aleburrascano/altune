/**
 * insertOptimisticTrackHome / optimisticTrack — prepend to the library-home
 * snapshot and bump its total (view-result-detail slice 15).
 */

import {
  insertOptimisticTrackHome,
  optimisticTrack,
  replaceOptimisticTrackHome,
} from '../save-cache';
import type { ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

function _home(items: TrackResponse[]): ListTracksResponse {
  return { items, total: items.length, limit: 50, offset: 0, has_more: false };
}

describe('insertOptimisticTrackHome', () => {
  it('leaves an absent library-home untouched rather than seeding it', () => {
    // An absent cache means the library errored or never fetched — NOT that it
    // is empty. Seeding here fabricated a one-track library over an error state
    // and staleTime: Infinity pinned it for the session. undefined = no-op.
    expect(insertOptimisticTrackHome(undefined, _track('opt'))).toBeUndefined();
  });

  it('still inserts into a genuinely empty library', () => {
    // The empty library is {items: []}, not undefined — optimistic feedback
    // must survive the guard above.
    const out = insertOptimisticTrackHome(_home([]), _track('opt'));
    expect(out?.items.map((t) => t.id)).toEqual(['opt']);
    expect(out?.total).toBe(1);
  });

  it('prepends and bumps the total when present', () => {
    const out = insertOptimisticTrackHome(_home([_track('a')]), _track('opt'));
    expect(out?.items.map((t) => t.id)).toEqual(['opt', 'a']);
    expect(out?.total).toBe(2);
  });

  it('is idempotent on the same id', () => {
    const seeded = _home([_track('opt')]);
    expect(insertOptimisticTrackHome(seeded, _track('opt'))).toBe(seeded);
  });
});

describe('replaceOptimisticTrackHome', () => {
  it('swaps the placeholder for the real row in the home snapshot', () => {
    const out = replaceOptimisticTrackHome(_home([_track('opt'), _track('a')]), 'opt', _track('real'));
    expect(out?.items.map((t) => t.id)).toEqual(['real', 'a']);
  });

  it('dedups when the SSE already inserted the real row', () => {
    // Race: SSE upserted `real` before onSuccess; the placeholder must not
    // produce a second `real` entry, and total drops back to 1.
    const out = replaceOptimisticTrackHome(_home([_track('real'), _track('opt')]), 'opt', _track('real'));
    expect(out?.items.map((t) => t.id)).toEqual(['real']);
    expect(out?.total).toBe(1);
  });
});

function _track(id: string): TrackResponse {
  return {
    id,
    title: `T ${id}`,
    artist: 'A',
    album: null,
    duration_seconds: null,
    added_at: '2026-05-01T12:00:00Z',
    acquisition_status: 'pending',
    artwork_url: null,
    year: null,
    genre: null,
    track_number: null,
    album_artist: null,
    isrc: null,
    audio_ref: null,
    failure_reason: null,
  };
}

describe('optimisticTrack', () => {
  it('maps a create request into a pending TrackResponse', () => {
    const t = optimisticTrack(
      { title: 'Midnight City', artist: 'M83', album: 'A', duration_seconds: 244, artwork_url: null, isrc: null, year: null, genre: null, album_artist: null, track_number: null },
      '2026-05-30T00:00:00Z',
    );
    expect(t.title).toBe('Midnight City');
    expect(t.acquisition_status).toBe('pending');
    expect(t.id).toContain('optimistic:');
  });
});
