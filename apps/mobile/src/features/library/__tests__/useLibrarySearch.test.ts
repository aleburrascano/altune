import { act, renderHook } from '@testing-library/react-native';

import { useLibrarySearch } from '../hooks/useLibrarySearch';

describe('useLibrarySearch — a 300ms debounce over a 2-character minimum', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('starts with an empty input and no committed query', () => {
    const { result } = renderHook(() => useLibrarySearch());
    expect(result.current.inputValue).toBe('');
    expect(result.current.query).toBe('');
    expect(result.current.hasQuery).toBe(false);
  });

  it('reflects keystrokes in inputValue immediately, before any query is committed', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('da'));
    expect(result.current.inputValue).toBe('da');
    expect(result.current.query).toBe('');
  });

  it('holds the query empty through the debounce window and commits it only once the window elapses', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('daft'));

    act(() => jest.advanceTimersByTime(299));
    expect(result.current.query).toBe('');

    act(() => jest.advanceTimersByTime(1));
    expect(result.current.query).toBe('daft');
    expect(result.current.hasQuery).toBe(true);
  });

  it('never commits a single character, the boundary just below the two-character minimum', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('d'));
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('');
  });

  it('commits at exactly two characters, the minimum', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('da'));
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('da');
  });

  it('commits the trimmed text, so surrounding whitespace neither counts toward the minimum nor reaches the server', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('  daft  '));
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('daft');
  });

  it('restarts the debounce on each keystroke, committing once for the final value rather than for each', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('da'));
    act(() => jest.advanceTimersByTime(200));
    act(() => result.current.onChangeText('daft'));
    act(() => jest.advanceTimersByTime(200));
    expect(result.current.query).toBe('');
    act(() => jest.advanceTimersByTime(100));
    expect(result.current.query).toBe('daft');
  });

  it('clears a committed query the moment the input falls back below the minimum', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('daft'));
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('daft');

    act(() => result.current.onChangeText('d'));
    expect(result.current.query).toBe('');
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('');
  });

  it('onSubmit commits the trimmed input at once, bypassing the debounce', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('  daft  '));
    act(() => result.current.onSubmit());
    expect(result.current.query).toBe('daft');
  });

  it('onSubmit cancels a pending debounce so a stale timer cannot overwrite the submitted query', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('daft'));
    act(() => result.current.onSubmit());
    act(() => result.current.onChangeText('daftx'));
    act(() => result.current.onSubmit());
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('daftx');
  });

  it('onClear resets the input and the committed query and cancels a pending debounce', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('daft'));
    act(() => result.current.onClear());
    expect(result.current.inputValue).toBe('');
    expect(result.current.query).toBe('');
    expect(result.current.hasQuery).toBe(false);
    act(() => jest.advanceTimersByTime(300));
    expect(result.current.query).toBe('');
  });
});
