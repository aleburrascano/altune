import { _viewForState, kindLabel, resultKey, type DiscoverHookState } from '../state';

import type {
  DiscoveryKind,
  DiscoveryResult,
  DiscoverySearchResponse,
} from '@shared/api-client/discovery';

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'The Title',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    sources: [{ provider: 'spotify', external_id: 'ext-1', url: 'https://x' }],
    extras: {},
    ...overrides,
  };
}

function responseFixture(results: DiscoveryResult[]): DiscoverySearchResponse {
  return {
    query: 'q',
    query_norm: 'q',
    results,
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: results.length,
    offset: 0,
    has_more: false,
  };
}

function hookState(overrides: Partial<DiscoverHookState> = {}): DiscoverHookState {
  return {
    query: 'radiohead',
    isLoading: false,
    data: undefined,
    error: null,
    ...overrides,
  };
}

describe('_viewForState maps hook state to the five-state union', () => {
  it('returns empty-no-query when the query is blank, even while loading', () => {
    const view = _viewForState(hookState({ query: '', isLoading: true }));

    expect(view).toBe('empty-no-query');
  });

  it('returns empty-no-query when the query is only whitespace', () => {
    expect(_viewForState(hookState({ query: '   ' }))).toBe('empty-no-query');
  });

  it('returns loading when the first fetch is in flight and no data has arrived', () => {
    const view = _viewForState(hookState({ isLoading: true, data: undefined }));

    expect(view).toBe('loading');
  });

  it('returns full-error when the first fetch failed and no data has arrived', () => {
    const view = _viewForState(
      hookState({ isLoading: false, data: undefined, error: new Error('boom') }),
    );

    expect(view).toBe('full-error');
  });

  it('returns zero-results when data arrived with an empty result set', () => {
    const view = _viewForState(hookState({ data: responseFixture([]) }));

    expect(view).toBe('zero-results');
  });

  it('returns results when data arrived with at least one result', () => {
    const view = _viewForState(hookState({ data: responseFixture([resultFixture()]) }));

    expect(view).toBe('results');
  });

  it('prefers loading over full-error while both are true and no data has arrived', () => {
    const view = _viewForState(
      hookState({ isLoading: true, data: undefined, error: new Error('boom') }),
    );

    expect(view).toBe('loading');
  });

  it('keeps showing results while a background refetch is in flight over existing data', () => {
    const view = _viewForState(
      hookState({ isLoading: true, data: responseFixture([resultFixture()]) }),
    );

    expect(view).toBe('results');
  });

  it('keeps showing results when a background error lands over existing data', () => {
    const view = _viewForState(
      hookState({ error: new Error('boom'), data: responseFixture([resultFixture()]) }),
    );

    expect(view).toBe('results');
  });

  it('shows zero-results, not full-error, when an error lands over an empty data set', () => {
    const view = _viewForState(
      hookState({ error: new Error('boom'), data: responseFixture([]) }),
    );

    expect(view).toBe('zero-results');
  });

  it('falls through to results, not full-error, when a live query has no data, error or loading', () => {
    const view = _viewForState(
      hookState({ isLoading: false, data: undefined, error: null }),
    );

    expect(view).toBe('results');
  });
});

describe('kindLabel renders the user-facing kind label', () => {
  const cases: Array<[DiscoveryKind, string, string]> = [
    ['artist', 'Artist', 'Artists'],
    ['album', 'Album', 'Albums'],
    ['track', 'Track', 'Tracks'],
  ];

  it.each(cases)('labels %s as singular by default', (kind, singular) => {
    expect(kindLabel(kind)).toBe(singular);
  });

  it.each(cases)('labels %s as singular when plural is explicitly false', (kind, singular) => {
    expect(kindLabel(kind, { plural: false })).toBe(singular);
  });

  it.each(cases)('labels %s as plural when plural is true', (kind, _singular, plural) => {
    expect(kindLabel(kind, { plural: true })).toBe(plural);
  });

  it('never surfaces the banned noun for the track kind', () => {
    expect(kindLabel('track')).not.toMatch(/song/i);
    expect(kindLabel('track', { plural: true })).not.toMatch(/song/i);
  });
});

describe('resultKey builds a stable key with source-aware fallbacks', () => {
  it('uses kind, first provider and external id when a source with an id is present', () => {
    const key = resultKey(
      resultFixture({
        kind: 'album',
        sources: [{ provider: 'tidal', external_id: 'abc', url: 'u' }],
      }),
      3,
    );

    expect(key).toBe('album-tidal-abc');
  });

  it('falls back to title and index when the first source has an empty external id', () => {
    const key = resultKey(
      resultFixture({
        kind: 'track',
        title: 'Karma Police',
        sources: [{ provider: 'spotify', external_id: '', url: 'u' }],
      }),
      2,
    );

    expect(key).toBe('track-spotify-Karma Police-2');
  });

  it('falls back to a placeholder provider and title-index when there is no source', () => {
    const key = resultKey(
      resultFixture({ kind: 'artist', title: 'Thom Yorke', sources: [] }),
      5,
    );

    expect(key).toBe('artist-x-Thom Yorke-5');
  });
});
