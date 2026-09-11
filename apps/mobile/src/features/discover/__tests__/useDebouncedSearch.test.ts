import { act, renderHook } from '@testing-library/react-native';

import { setSearchState } from '../search-state';
import { useDebouncedSearch } from '../hooks/useDebouncedSearch';

const OPTIONS = { debounceMs: 300, minChars: 2 };

beforeEach(() => {
  setSearchState('', '');
  jest.useFakeTimers();
});

afterEach(() => {
  jest.runOnlyPendingTimers();
  jest.useRealTimers();
});

describe('useDebouncedSearch commits typed queries only after the debounce window', () => {
  it('starts as a non-explicit submit before any interaction', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    expect(result.current.isExplicitSubmit).toBe(false);
  });

  it('holds the committed query until the debounce window elapses', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('radiohead');
    });

    expect(result.current.inputValue).toBe('radiohead');
    expect(result.current.committedQuery).toBe('');

    act(() => {
      jest.advanceTimersByTime(299);
    });
    expect(result.current.committedQuery).toBe('');

    act(() => {
      jest.advanceTimersByTime(1);
    });
    expect(result.current.committedQuery).toBe('radiohead');
  });

  it('marks a debounced commit as not an explicit submit', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('radiohead');
    });
    act(() => {
      jest.advanceTimersByTime(300);
    });

    expect(result.current.isExplicitSubmit).toBe(false);
  });

  it('commits a query exactly at the minimum character count', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('ab');
    });
    act(() => {
      jest.advanceTimersByTime(300);
    });

    expect(result.current.committedQuery).toBe('ab');
  });

  it('commits the whitespace-trimmed query after a debounced change', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('  radiohead  ');
    });
    act(() => {
      jest.advanceTimersByTime(300);
    });

    expect(result.current.committedQuery).toBe('radiohead');
  });

  it('cancels a superseded keystroke so the stale query never commits', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('aa');
    });
    act(() => {
      jest.advanceTimersByTime(200);
    });
    act(() => {
      result.current.onChangeText('bb');
    });
    act(() => {
      jest.advanceTimersByTime(100);
    });
    expect(result.current.committedQuery).toBe('');

    act(() => {
      jest.advanceTimersByTime(200);
    });
    expect(result.current.committedQuery).toBe('bb');
  });

  it('never commits a query shorter than the minimum character count', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('a');
    });
    act(() => {
      jest.advanceTimersByTime(1000);
    });

    expect(result.current.inputValue).toBe('a');
    expect(result.current.committedQuery).toBe('');
  });

  it('commits only the final keystroke when typing faster than the window', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('rad');
    });
    act(() => {
      jest.advanceTimersByTime(200);
    });
    act(() => {
      result.current.onChangeText('radiohead');
    });
    act(() => {
      jest.advanceTimersByTime(300);
    });

    expect(result.current.committedQuery).toBe('radiohead');
  });

  it('clears a committed query immediately when the input is emptied', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('radiohead');
    });
    act(() => {
      jest.advanceTimersByTime(300);
    });
    act(() => {
      result.current.onChangeText('');
    });

    expect(result.current.committedQuery).toBe('');
    expect(result.current.isExplicitSubmit).toBe(false);
  });
});

describe('useDebouncedSearch treats explicit actions as history-saving submits', () => {
  it('commits a trimmed query immediately on submit and marks it explicit', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.onChangeText('  radiohead  ');
    });
    act(() => {
      result.current.onSubmit();
    });

    expect(result.current.committedQuery).toBe('radiohead');
    expect(result.current.isExplicitSubmit).toBe(true);
  });

  it('commits a chosen suggestion immediately and marks it explicit', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.setQuery('the national');
    });

    expect(result.current.inputValue).toBe('the national');
    expect(result.current.committedQuery).toBe('the national');
    expect(result.current.isExplicitSubmit).toBe(true);
  });

  it('resets to an empty, non-explicit state on clear', () => {
    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    act(() => {
      result.current.setQuery('radiohead');
    });
    act(() => {
      result.current.onClear();
    });

    expect(result.current.inputValue).toBe('');
    expect(result.current.committedQuery).toBe('');
    expect(result.current.isExplicitSubmit).toBe(false);
  });
});

describe('useDebouncedSearch rehydrates the preserved search on mount', () => {
  it('starts from the last saved query and input value', () => {
    setSearchState('saved query', 'saved input');

    const { result } = renderHook(() => useDebouncedSearch(OPTIONS));

    expect(result.current.committedQuery).toBe('saved query');
    expect(result.current.inputValue).toBe('saved input');
  });
});
