import { _viewForState, kindLabel, resultKey } from '../state';

import type {
  DiscoveryKind,
  DiscoveryResult,
  DiscoverySearchResponse,
} from '@shared/api-client/discovery';

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
  sections: [],
  providers: [],
  partial: false,
  cache: { hit: false, fetched_at: null },
  total: 0,
  offset: 0,
  has_more: false,
});

describe('_viewForState', () => {
  it('returns empty-no-query when query is blank', () => {
    expect(_viewForState({ query: '', isLoading: false, data: undefined, error: null })).toBe(
      'empty-no-query',
    );
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

describe('kindLabel', () => {
  it('maps track to the UI noun Song, singular and plural', () => {
    expect(kindLabel('track')).toBe('Song');
    expect(kindLabel('track', { plural: true })).toBe('Songs');
  });

  it('maps album and artist', () => {
    expect(kindLabel('album')).toBe('Album');
    expect(kindLabel('album', { plural: true })).toBe('Albums');
    expect(kindLabel('artist')).toBe('Artist');
    expect(kindLabel('artist', { plural: true })).toBe('Artists');
  });
});

describe('resultKey', () => {
  it('keys on kind + provider identity when a source exists', () => {
    const r = {
      ..._result('track', 'Midnight City'),
      sources: [{ provider: 'deezer', external_id: '42', url: 'https://x/42' }],
    };
    expect(resultKey(r, 3)).toBe('track-deezer-42');
  });

  it('falls back to title+index so sourceless same-title results cannot collide', () => {
    const a = _result('track', 'Intro');
    const b = _result('track', 'Intro');
    expect(resultKey(a, 0)).toBe('track-x-Intro-0');
    expect(resultKey(a, 0)).not.toBe(resultKey(b, 1));
  });

  it('falls back to title+index when external_id is empty', () => {
    const r = {
      ..._result('album', 'Hurry Up'),
      sources: [{ provider: 'itunes', external_id: '', url: 'https://x' }],
    };
    expect(resultKey(r, 2)).toBe('album-itunes-Hurry Up-2');
  });
});

