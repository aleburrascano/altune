import { act, renderHook } from '@testing-library/react-native';

import { setSearchState } from '../search-state';
import { useDebouncedSearch } from '../hooks/useDebouncedSearch';
import { MAX_QUERY_LENGTH } from '../searchLimits';

const OPTIONS = { debounceMs: 300, minChars: 2 };

beforeEach(() => {
  setSearchState('', '');
  jest.useFakeTimers();
});

afterEach(() => {
  jest.runOnlyPendingTimers();
  jest.useRealTimers();
});

describe('useDebouncedSearch applies one trim and length rule to every commit path', () => {
  it('setQuery trims the committed query and the input', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.setQuery('  ab  ');
    });

    expect(result.current.committedQuery).toBe('ab');
    expect(result.current.inputValue).toBe('ab');
  });

  it('setQuery commits nothing below the minimum length', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.setQuery('x');
    });

    expect(result.current.committedQuery).toBe('');
    expect(result.current.isExplicitSubmit).toBe(false);
  });

  it('onChangeText never commits more than MAX_QUERY_LENGTH characters', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('a'.repeat(5000));
      jest.advanceTimersByTime(300);
    });

    expect(result.current.committedQuery).toHaveLength(MAX_QUERY_LENGTH);
    expect(result.current.inputValue).toHaveLength(MAX_QUERY_LENGTH);
  });

  it('setQuery never commits more than MAX_QUERY_LENGTH characters', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.setQuery('a'.repeat(5000));
    });

    expect(result.current.committedQuery).toHaveLength(MAX_QUERY_LENGTH);
  });
});
