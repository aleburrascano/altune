import { fireEvent, render, screen } from '@testing-library/react-native';
import { Keyboard, Platform } from 'react-native';

import type { DiscoverLogic } from '../hooks/useDiscoverLogic';
import { DiscoverScreen } from '../ui/DiscoverScreen';

const mockPush = jest.fn();
const focusEffects: (() => (() => void) | void)[] = [];

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useFocusEffect: (effect: () => (() => void) | void) => {
    focusEffects.push(effect);
  },
}));

const mockFocus = jest.fn();
jest.mock('@shared/ui/primitives/SearchBar', () => {
  const { forwardRef: fr, useImperativeHandle: useImp } = jest.requireActual('react');
  return {
    SearchBar: fr((_props: unknown, ref: unknown) => {
      useImp(ref, () => ({ focus: mockFocus }));
      return null;
    }),
  };
});

jest.mock('../ui/DiscoverBody', () => ({ DiscoverBody: () => null }));
jest.mock('../ui/SuggestionsList', () => ({ SuggestionsList: () => null }));

function mockDiscoverLogic(): DiscoverLogic {
  return {
    inputValue: '',
    committedQuery: '',
    pending: false,
    onChangeText: jest.fn(),
    onSubmit: jest.fn(),
    onClear: jest.fn(),
    isFocused: false,
    setIsFocused: jest.fn(),
    showSuggestions: false,
    suggestionItems: [],
    onSuggestionSelect: jest.fn(),
    view: 'empty-no-query',
    resultsIncomplete: false,
    searchData: undefined,
    historyItems: [],
    filter: 'all',
    setFilter: jest.fn(),
    onHistoryTap: jest.fn(),
    onResultTap: jest.fn(),
    impression: {
      onImpression: jest.fn(),
      onImpressionEnd: jest.fn(),
    } as unknown as DiscoverLogic['impression'],
    onRetry: jest.fn(),
    searchError: null,
    onEndReached: jest.fn(),
    hasNextPage: false,
    isFetchingNextPage: false,
    onRefresh: jest.fn(),
    isRefreshing: false,
    correction: null,
    onSearchOriginal: jest.fn(),
    onClearHistory: jest.fn(),
    nextPageFailed: false,
    onRetryNextPage: jest.fn(),
    clearHistoryFailed: false,
    refreshFailed: false,
  };
}

jest.mock('../hooks/useDiscoverLogic', () => ({
  useDiscoverLogic: () => mockDiscoverLogic(),
}));

const originalOS = Platform.OS;

describe('DiscoverScreen tap-to-dismiss-keyboard area', () => {
  let dismiss: jest.SpyInstance;

  beforeEach(() => {
    dismiss = jest.spyOn(Keyboard, 'dismiss').mockImplementation(() => undefined);
  });

  afterEach(() => {
    dismiss.mockRestore();
    Platform.OS = originalOS;
  });

  it('dismisses the keyboard when the screen body is tapped on native', () => {
    Platform.OS = 'ios';
    render(<DiscoverScreen />);
    fireEvent.press(screen.getByTestId('discover-screen-body'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('never blurs the search input on web, where a click in the input reaches the screen body', () => {
    Platform.OS = 'web';
    render(<DiscoverScreen />);
    fireEvent.press(screen.getByTestId('discover-screen-body'));
    expect(dismiss).not.toHaveBeenCalled();
  });
});
