/**
 * insertOptimisticTrack / optimisticTrack — prepend to the first library page
 * and bump its total; seed a fresh page when the cache is empty
 * (view-result-detail slice 15).
 */

import type { InfiniteData } from '@tanstack/react-query';

import {
  insertOptimisticTrack,
  insertOptimisticTrackHome,
  optimisticTrack,
  replaceOptimisticTrackHome,
  replaceOptimisticTrackInfinite,
} from '../save-cache';
import type { ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

function _home(items: TrackResponse[]): ListTracksResponse {
  return { items, total: items.length, limit: 50, offset: 0, has_more: false };
}

describe('insertOptimisticTrackHome', () => {
  it('creates the snapshot when library-home is absent', () => {
    const out = insertOptimisticTrackHome(undefined, _track('opt'));
    expect(out.items.map((t) => t.id)).toEqual(['opt']);
    expect(out.total).toBe(1);
  });

  it('prepends and bumps the total when present', () => {
    const out = insertOptimisticTrackHome(_home([_track('a')]), _track('opt'));
    expect(out.items.map((t) => t.id)).toEqual(['opt', 'a']);
    expect(out.total).toBe(2);
  });

  it('is idempotent on the same id', () => {
    const seeded = _home([_track('opt')]);
    expect(insertOptimisticTrackHome(seeded, _track('opt'))).toBe(seeded);
  });
});

describe('replaceOptimisticTrack*', () => {
  it('swaps the placeholder for the real row in the home snapshot', () => {
    const out = replaceOptimisticTrackHome(_home([_track('opt'), _track('a')]), 'opt', _track('real'));
    expect(out?.items.map((t) => t.id)).toEqual(['real', 'a']);
  });

  it('swaps the placeholder across infinite-query pages', () => {
    const out = replaceOptimisticTrackInfinite(_data([_track('opt')], 1), 'opt', _track('real'));
    expect(out?.pages[0]?.items.map((t) => t.id)).toEqual(['real']);
  });

  it('dedups when the SSE already inserted the real row (home)', () => {
    // Race: SSE upserted `real` before onSuccess; the placeholder must not
    // produce a second `real` entry, and total drops back to 1.
    const out = replaceOptimisticTrackHome(_home([_track('real'), _track('opt')]), 'opt', _track('real'));
    expect(out?.items.map((t) => t.id)).toEqual(['real']);
    expect(out?.total).toBe(1);
  });

  it('dedups when the SSE already inserted the real row (infinite)', () => {
    const out = replaceOptimisticTrackInfinite(_data([_track('real'), _track('opt')], 2), 'opt', _track('real'));
    expect(out?.pages.flatMap((p) => p.items.map((t) => t.id))).toEqual(['real']);
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

function _data(items: TrackResponse[], total: number): InfiniteData<ListTracksResponse> {
  return {
    pageParams: [0],
    pages: [{ items, total, limit: 50, offset: 0, has_more: false }],
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

describe('insertOptimisticTrack', () => {
  it('prepends to the first page and bumps total', () => {
    const data = _data([_track('a'), _track('b')], 2);
    const next = insertOptimisticTrack(data, _track('new'));
    expect(next.pages[0]?.items.map((t) => t.id)).toEqual(['new', 'a', 'b']);
    expect(next.pages[0]?.total).toBe(3);
  });

  it('seeds a fresh first page when the cache is empty', () => {
    const next = insertOptimisticTrack(undefined, _track('new'));
    expect(next.pages).toHaveLength(1);
    expect(next.pages[0]?.items.map((t) => t.id)).toEqual(['new']);
    expect(next.pages[0]?.total).toBe(1);
  });

  it('does not mutate the input data', () => {
    const data = _data([_track('a')], 1);
    insertOptimisticTrack(data, _track('new'));
    expect(data.pages[0]?.items.map((t) => t.id)).toEqual(['a']);
  });
});
