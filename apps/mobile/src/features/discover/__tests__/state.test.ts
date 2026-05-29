/**
 * State machine helpers for the discover feature — slice 44.
 */

import {
  _cap,
  _groupByKind,
  _sectionOrder,
  _shouldShowPartialBanner,
  _topResult,
  _viewForState,
} from '../state';

import type {
  DiscoveryKind,
  DiscoveryResult,
  DiscoverySearchResponse,
} from '../../../shared/api-client/discovery';

const _result = (kind: DiscoveryKind, title: string): DiscoveryResult => ({
  kind,
  title,
  subtitle: null,
  image_url: null,
  confidence: 'low',
  sources: [],
  extras: {},
});

const _empty = (): DiscoverySearchResponse => ({
  query: 'q',
  query_norm: 'q',
  results: [],
  providers: [],
  partial: false,
  cache: { hit: false, fetched_at: null },
});

describe('_viewForState', () => {
  it('returns empty-no-query when query is blank', () => {
    expect(
      _viewForState({ query: '', isLoading: false, data: undefined, error: null }),
    ).toBe('empty-no-query');
    expect(
      _viewForState({
        query: '   ',
        isLoading: false,
        data: undefined,
        error: null,
      }),
    ).toBe('empty-no-query');
  });

  it('returns loading when query present and no data yet', () => {
    expect(
      _viewForState({
        query: 'beatles',
        isLoading: true,
        data: undefined,
        error: null,
      }),
    ).toBe('loading');
  });

  it('returns full-error when query present and error with no data', () => {
    expect(
      _viewForState({
        query: 'beatles',
        isLoading: false,
        data: undefined,
        error: new Error('boom'),
      }),
    ).toBe('full-error');
  });

  it('returns zero-results when data has empty results array', () => {
    expect(
      _viewForState({
        query: 'beatles',
        isLoading: false,
        data: _empty(),
        error: null,
      }),
    ).toBe('zero-results');
  });

  it('returns results when data has at least one entry', () => {
    const data = {
      ..._empty(),
      results: [
        {
          kind: 'track' as const,
          title: 'Let It Be',
          subtitle: 'The Beatles',
          image_url: null,
          confidence: 'high' as const,
          sources: [],
          extras: {},
        },
      ],
    };
    expect(
      _viewForState({
        query: 'beatles',
        isLoading: false,
        data,
        error: null,
      }),
    ).toBe('results');
  });
});

describe('_shouldShowPartialBanner', () => {
  it('returns false when data is undefined', () => {
    expect(_shouldShowPartialBanner(undefined)).toBe(false);
  });

  it('returns false when all providers are ok', () => {
    const data: DiscoverySearchResponse = {
      ..._empty(),
      providers: [
        {
          provider: 'deezer',
          status: 'ok',
          result_count: 1,
          latency_ms: 100,
        },
      ],
    };
    expect(_shouldShowPartialBanner(data)).toBe(false);
  });

  it('returns true when any provider is not ok', () => {
    const data: DiscoverySearchResponse = {
      ..._empty(),
      partial: true,
      providers: [
        {
          provider: 'deezer',
          status: 'ok',
          result_count: 1,
          latency_ms: 100,
        },
        {
          provider: 'soundcloud',
          status: 'timeout',
          result_count: 0,
          latency_ms: 1500,
        },
      ],
    };
    expect(_shouldShowPartialBanner(data)).toBe(true);
  });
});

describe('_groupByKind', () => {
  it('returns empty buckets for empty input', () => {
    expect(_groupByKind([])).toEqual({ albums: [], songs: [], artists: [] });
  });

  it('partitions by kind, tracks landing in songs', () => {
    const grouped = _groupByKind([
      _result('artist', 'Che'),
      _result('album', 'Rest in Bass'),
      _result('track', 'Some Song'),
    ]);
    expect(grouped.albums.map((r) => r.title)).toEqual(['Rest in Bass']);
    expect(grouped.songs.map((r) => r.title)).toEqual(['Some Song']);
    expect(grouped.artists.map((r) => r.title)).toEqual(['Che']);
  });

  it('preserves backend order within each kind', () => {
    const grouped = _groupByKind([
      _result('album', 'A1'),
      _result('album', 'A2'),
      _result('album', 'A3'),
    ]);
    expect(grouped.albums.map((r) => r.title)).toEqual(['A1', 'A2', 'A3']);
  });
});

describe('_topResult', () => {
  it('returns null for empty input', () => {
    expect(_topResult([])).toBeNull();
  });

  it('returns the first entry (backend-ranked top result)', () => {
    const top = _result('album', 'Rest in Bass');
    expect(_topResult([top, _result('track', 'Other')])).toBe(top);
  });
});

describe('_cap', () => {
  it('returns at most `cap` items, preserving order', () => {
    const items = Array.from({ length: 15 }, (_, i) => i);
    expect(_cap(items, 10)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
  });

  it('returns all items when fewer than the cap', () => {
    expect(_cap([1, 2, 3], 10)).toEqual([1, 2, 3]);
  });
});

describe('_sectionOrder', () => {
  it('orders sections by the kind whose best member ranks earliest', () => {
    // A song query: a track ranks first, so Songs (song) leads, then albums.
    const results = [
      _result('track', 'Creep'),
      _result('album', 'Creep EP'),
      _result('track', 'Creep (Live)'),
      _result('artist', 'Creep'),
    ];
    expect(_sectionOrder(results)).toEqual(['song', 'album', 'artist']);
  });

  it('omits kinds with no results', () => {
    expect(_sectionOrder([_result('artist', 'Che'), _result('album', 'Rest in Bass')])).toEqual([
      'artist',
      'album',
    ]);
  });

  it('returns empty for no results', () => {
    expect(_sectionOrder([])).toEqual([]);
  });
});
