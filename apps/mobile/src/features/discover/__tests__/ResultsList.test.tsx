import React from 'react';
import { Platform, Text } from 'react-native';
import { fireEvent, render, screen } from '@testing-library/react-native';

import { ResultsList, type ResultsCommonProps } from '../ui/ResultsList';

const NATIVE_OS = Platform.OS;

let mockWindowWidth = 390;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 900, scale: 2, fontScale: 1 }),
}));

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

describe('pairing the header with the first item on a wide screen', () => {
  // The wide pairing layout only applies on web (see useWideWebLayout), so
  // these tests run under Platform.OS = 'web', restored after each one.
  beforeEach(() => {
    Platform.OS = 'web';
  });

  afterEach(() => {
    Platform.OS = NATIVE_OS;
    mockWindowWidth = 390;
  });

  it('renders the header beside the first item once, at 1440px', () => {
    mockWindowWidth = 1440;
    render(
      <ResultsList
        data={[1, 2, 3]}
        keyExtractor={(item) => String(item)}
        renderItem={({ item }) => <Text testID={`item-${item}`}>{item}</Text>}
        headerExtra={<Text testID="header-extra">H</Text>}
        pairFirstItemWithHeader
        common={common({})}
      />,
    );

    expect(screen.getByTestId('discover-top-pair')).toBeTruthy();
    expect(screen.getAllByTestId('item-1')).toHaveLength(1);
    expect(screen.getByTestId('item-2')).toBeTruthy();
  });

  it('renders the header above the full list unchanged on a compact screen', () => {
    mockWindowWidth = 390;
    render(
      <ResultsList
        data={[1, 2, 3]}
        keyExtractor={(item) => String(item)}
        renderItem={({ item }) => <Text testID={`item-${item}`}>{item}</Text>}
        headerExtra={<Text testID="header-extra">H</Text>}
        pairFirstItemWithHeader
        common={common({})}
      />,
    );

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('header-extra')).toBeTruthy();
    expect(screen.getByTestId('item-1')).toBeTruthy();
  });

  it('does not pair when the list has no items even at 1440px', () => {
    mockWindowWidth = 1440;
    render(
      <ResultsList
        data={[]}
        keyExtractor={(item) => String(item)}
        renderItem={() => null}
        headerExtra={<Text testID="header-extra">H</Text>}
        pairFirstItemWithHeader
        common={common({})}
      />,
    );

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('header-extra')).toBeTruthy();
  });

  it('does not pair at 1440px when pairing was not requested', () => {
    mockWindowWidth = 1440;
    render(
      <ResultsList
        data={[1, 2]}
        keyExtractor={(item) => String(item)}
        renderItem={({ item }) => <Text testID={`item-${item}`}>{item}</Text>}
        headerExtra={<Text testID="header-extra">H</Text>}
        common={common({})}
      />,
    );

    expect(screen.queryByTestId('discover-top-pair')).toBeNull();
    expect(screen.getByTestId('header-extra')).toBeTruthy();
    expect(screen.getByTestId('item-1')).toBeTruthy();
  });
});
