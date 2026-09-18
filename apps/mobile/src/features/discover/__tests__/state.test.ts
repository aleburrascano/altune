import {
  _resultsIncompleteForState,
  _searchAnnouncement,
  _viewForState,
  type DiscoverHookState,
} from '../state';
import { resultFixture } from './fixtures';

import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

function responseFixture(results: DiscoveryResult[], partial = false): DiscoverySearchResponse {
  return {
    query: 'q',
    query_norm: 'q',
    results,
    sections: [],
    providers: [],
    partial,
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

describe('_resultsIncompleteForState tells a degraded (partial) search apart from a healthy one', () => {
  it('is true when shown results come from a partial response', () => {
    const state = hookState({ data: responseFixture([resultFixture()], true) });

    expect(_resultsIncompleteForState(state)).toBe(true);
  });

  it('is false for the same results from a fully healthy response', () => {
    const state = hookState({ data: responseFixture([resultFixture()], false) });

    expect(_resultsIncompleteForState(state)).toBe(false);
  });

  it('is true for a partial zero-results response, where the missing provider may hold the match', () => {
    expect(_resultsIncompleteForState(hookState({ data: responseFixture([], true) }))).toBe(true);
  });

  it('is false when the query is blank, since no response is shown', () => {
    const state = hookState({ query: ' ', data: responseFixture([], true) });

    expect(_resultsIncompleteForState(state)).toBe(false);
  });

  it('is false when no data has arrived', () => {
    expect(_resultsIncompleteForState(hookState({ data: undefined }))).toBe(false);
  });
});

describe('_searchAnnouncement tells screen readers when results may be incomplete', () => {
  it('appends the incomplete note to result and zero-result announcements', () => {
    expect(_searchAnnouncement('results', 3, true)).toBe('3 results. Some results may be missing');
    expect(_searchAnnouncement('zero-results', 0, true)).toBe(
      'No matches. Some results may be missing',
    );
  });

  it('leaves healthy announcements unchanged', () => {
    expect(_searchAnnouncement('results', 1)).toBe('1 result');
    expect(_searchAnnouncement('zero-results', 0, false)).toBe('No matches');
  });
});
