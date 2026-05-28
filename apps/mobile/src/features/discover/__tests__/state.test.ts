/**
 * State machine helpers for the discover feature — slice 44.
 */

import { _shouldShowPartialBanner, _viewForState } from '../state';

import type { DiscoverySearchResponse } from '../../../shared/api-client/discovery';

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
