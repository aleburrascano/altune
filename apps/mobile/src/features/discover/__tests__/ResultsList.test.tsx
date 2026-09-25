import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { ResultsList, type ResultsCommonProps } from '../ui/ResultsList';

const common = (over: Partial<ResultsCommonProps>): ResultsCommonProps => ({
  onResultTap: jest.fn(),
  impression: {
    viewabilityConfig: { itemVisiblePercentThreshold: 50 },
    onViewableItemsChanged: jest.fn(),
  },
  onRefresh: jest.fn(),
  isRefreshing: false,
  onEndReached: jest.fn(),
  isFetchingNextPage: false,
  correction: null,
  onSearchOriginal: jest.fn(),
  ...over,
});

describe('next page failure', () => {
  it('shows a retry footer that calls onRetryNextPage', () => {
    const onRetryNextPage = jest.fn();
    render(
      <ResultsList
        data={[]}
        keyExtractor={(_, i) => String(i)}
        renderItem={() => null}
        common={common({ nextPageFailed: true, onRetryNextPage })}
      />,
    );
    fireEvent.press(screen.getByTestId('discover-load-more-error'));
    expect(onRetryNextPage).toHaveBeenCalledTimes(1);
  });

  it('shows no retry footer when the next page did not fail', () => {
    render(
      <ResultsList
        data={[]}
        keyExtractor={(_, i) => String(i)}
        renderItem={() => null}
        common={common({})}
      />,
    );
    expect(screen.queryByTestId('discover-load-more-error')).toBeNull();
  });
});
