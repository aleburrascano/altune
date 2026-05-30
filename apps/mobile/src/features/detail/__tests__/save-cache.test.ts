/**
 * insertOptimisticTrack / optimisticTrack — prepend to the first library page
 * and bump its total; seed a fresh page when the cache is empty
 * (view-result-detail slice 15).
 */

import type { InfiniteData } from '@tanstack/react-query';

import { insertOptimisticTrack, optimisticTrack } from '../save-cache';
import type { ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

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
      { title: 'Midnight City', artist: 'M83', album: 'A', duration_seconds: 244, artwork_url: null },
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
