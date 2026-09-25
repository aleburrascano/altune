import React from 'react';
import { render, screen } from '@testing-library/react-native';

import { DiscoverBody } from '../ui/DiscoverBody';
import { resultFixture } from './fixtures';

import type { DiscoverView } from '../state';

function renderBody(view: DiscoverView, resultsIncomplete: boolean) {
  const results = view === 'results' ? [resultFixture()] : [];
  render(
    <DiscoverBody
      view={view}
      // Rows need a PlaybackProvider; the banner does not depend on them, so no sections.
      searchData={{ results, sections: [] }}
      resultsIncomplete={resultsIncomplete}
      historyItems={[]}
      filter="all"
      onFilterChange={jest.fn()}
      onHistoryTap={jest.fn()}
      onClearHistory={jest.fn()}
      onResultTap={jest.fn()}
      impression={{
        viewabilityConfig: { itemVisiblePercentThreshold: 50 },
        onViewableItemsChanged: jest.fn(),
      }}
      onRetry={jest.fn()}
      onEndReached={jest.fn()}
      isFetchingNextPage={false}
      onRefresh={jest.fn()}
      isRefreshing={false}
      correction={null}
      onSearchOriginal={jest.fn()}
    />,
  );
}

describe('DiscoverBody flags a partial (degraded) search as possibly incomplete', () => {
  it('shows the incomplete-results banner over partial results', () => {
    renderBody('results', true);

    expect(screen.getByTestId('discover-results')).toBeTruthy();
    expect(screen.getByTestId('discover-incomplete-results')).toBeTruthy();
    expect(screen.getByText(/Some results may be missing/)).toBeTruthy();
  });

  it('renders no banner for the same results from a healthy search', () => {
    renderBody('results', false);

    expect(screen.getByTestId('discover-results')).toBeTruthy();
    expect(screen.queryByTestId('discover-incomplete-results')).toBeNull();
  });

  it('shows the banner on a partial zero-results search, so "No matches" is not taken as final', () => {
    renderBody('zero-results', true);

    expect(screen.getByTestId('discover-zero-results')).toBeTruthy();
    expect(screen.getByTestId('discover-incomplete-results')).toBeTruthy();
  });
});

describe('clear history failure', () => {
  function renderEmpty(clearHistoryFailed: boolean) {
    render(
      <DiscoverBody
        view="empty-no-query"
        searchData={undefined}
        historyItems={[{ query: 'old', query_norm: 'old' } as never]}
        filter="all"
        onFilterChange={jest.fn()}
        onHistoryTap={jest.fn()}
        onResultTap={jest.fn()}
        impression={{
          viewabilityConfig: { itemVisiblePercentThreshold: 50 },
          onViewableItemsChanged: jest.fn(),
        }}
        onRetry={jest.fn()}
        onEndReached={jest.fn()}
        isFetchingNextPage={false}
        onRefresh={jest.fn()}
        isRefreshing={false}
        correction={null}
        onSearchOriginal={jest.fn()}
        onClearHistory={jest.fn()}
        clearHistoryFailed={clearHistoryFailed}
      />,
    );
  }

  it('shows a message when the clear failed', () => {
    renderEmpty(true);
    expect(screen.getByTestId('discover-clear-history-error')).toBeTruthy();
  });

  it('shows no message otherwise', () => {
    renderEmpty(false);
    expect(screen.queryByTestId('discover-clear-history-error')).toBeNull();
  });
});
