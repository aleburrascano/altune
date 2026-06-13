/**
 * Pagination helpers — terminal condition + page flattening.
 *
 * Slice 9 of view-library. We test the pure helpers directly rather than
 * spinning up React Query + RNTL + QueryClientProvider — the helpers carry
 * the load-bearing logic (terminal condition for AC#3, item-order preservation
 * for AC#1) and React Query's own contract for `useInfiniteQuery` +
 * `getNextPageParam` is tested by their library.
 *
 * A complementary renderHook integration test can be added now that
 * jest-expo's preset works (apps/mobile/.npmrc forces nested install so
 * jest-expo + react-native are siblings); deferred as its own follow-up.
 */

import { _flattenPages, _nextOffsetFromPage } from '../hooks/useLibrary';
import type { ListTracksResponse, TrackResponse } from '../../../shared/api-client/types';

function _page(args: {
  items?: TrackResponse[];
  total?: number;
  limit?: number;
  offset?: number;
  has_more?: boolean;
}): ListTracksResponse {
  return {
    items: args.items ?? [],
    total: args.total ?? 0,
    limit: args.limit ?? 50,
    offset: args.offset ?? 0,
    has_more: args.has_more ?? false,
  };
}

function _track(id: string): TrackResponse {
  return {
    id,
    title: `Track ${id.slice(-3)}`,
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

describe('_nextOffsetFromPage', () => {
  it('returns undefined when has_more is false (terminal condition)', () => {
    const page = _page({ items: [_track('aaa')], offset: 0, has_more: false });
    expect(_nextOffsetFromPage(page)).toBeUndefined();
  });

  it('returns offset + items.length when has_more is true', () => {
    const page = _page({
      items: [_track('aaa'), _track('bbb'), _track('ccc')],
      offset: 50,
      has_more: true,
    });
    expect(_nextOffsetFromPage(page)).toBe(53);
  });

  it('handles empty page with has_more true (degenerate but possible during race)', () => {
    const page = _page({ items: [], offset: 100, has_more: true });
    expect(_nextOffsetFromPage(page)).toBe(100);
  });
});

describe('_flattenPages', () => {
  it('returns empty array when no pages', () => {
    expect(_flattenPages([])).toEqual([]);
  });

  it('concatenates items from multiple pages in fetch order', () => {
    const pages = [
      _page({ items: [_track('aaa'), _track('bbb')] }),
      _page({ items: [_track('ccc')] }),
      _page({ items: [_track('ddd'), _track('eee')] }),
    ];
    const flat = _flattenPages(pages);
    expect(flat.map((t) => t.id)).toEqual(['aaa', 'bbb', 'ccc', 'ddd', 'eee']);
  });

  it('preserves item order within each page', () => {
    const pages = [_page({ items: [_track('aaa'), _track('bbb'), _track('ccc')] })];
    expect(_flattenPages(pages).map((t) => t.id)).toEqual(['aaa', 'bbb', 'ccc']);
  });
});
