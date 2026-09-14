import { _viewForState, type DiscoverHookState } from '../state';
import { resultFixture } from './fixtures';

import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

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
    const view = _viewForState(hookState({ error: new Error('boom'), data: responseFixture([]) }));

    expect(view).toBe('zero-results');
  });

  it('falls through to results, not full-error, when a live query has no data, error or loading', () => {
    const view = _viewForState(hookState({ isLoading: false, data: undefined, error: null }));

    expect(view).toBe('results');
  });
});
