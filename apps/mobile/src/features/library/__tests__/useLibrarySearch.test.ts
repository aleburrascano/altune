import { act, renderHook } from '@testing-library/react-native';

import { useLibrarySearch } from '../hooks/useLibrarySearch';

jest.useFakeTimers();

describe('useLibrarySearch', () => {

  it('clears the committed query when the input drops below two characters', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('keep on lov'));
    act(() => jest.advanceTimersByTime(300));

    act(() => result.current.onChangeText('k'));

    expect(result.current.hasQuery).toBe(false);
  });

  it('onClear resets input and committed query', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('toto'));
    act(() => jest.advanceTimersByTime(300));

    act(() => result.current.onClear());

    expect(result.current.inputValue).toBe('');
    expect(result.current.hasQuery).toBe(false);
  });

  it('exposes the committed query for the no-results message', () => {
    const { result } = renderHook(() => useLibrarySearch());
    act(() => result.current.onChangeText('reo speedwagon'));
    act(() => jest.advanceTimersByTime(300));

    expect(result.current.query).toBe('reo speedwagon');
  });
});
