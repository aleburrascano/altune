import { act, renderHook } from '@testing-library/react-native';

import { MIN_QUERY_LENGTH } from '../searchLimits';
import { useSuggestionVisibility } from '../hooks/useSuggestionVisibility';

describe('useSuggestionVisibility shows suggestions only while the user is typing', () => {
  function setup(inputValue: string, count: number) {
    const search = {
      inputValue,
      onChangeText: jest.fn(),
      onSubmit: jest.fn(),
      setQuery: jest.fn(),
    };
    const hook = renderHook(({ n }: { n: number }) => useSuggestionVisibility(search, n), {
      initialProps: { n: count },
    });
    return { search, ...hook };
  }

  it('requires focus, a minimum-length query and at least one suggestion', () => {
    const { result, rerender } = setup('a'.repeat(MIN_QUERY_LENGTH), 1);
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.setIsFocused(true));
    expect(result.current.showSuggestions).toBe(true);

    rerender({ n: 0 });
    expect(result.current.showSuggestions).toBe(false);
  });

  it('stays closed for a focused query left below the minimum length by trimming', () => {
    const { result } = setup(` ${'a'.repeat(MIN_QUERY_LENGTH - 1)} `, 1);

    act(() => result.current.setIsFocused(true));

    expect(result.current.showSuggestions).toBe(false);
  });

  it('hides on submit and on suggestion select, and reopens on typing', () => {
    const { result, search } = setup('radio', 3);
    act(() => result.current.setIsFocused(true));

    act(() => result.current.onSubmit());
    expect(search.onSubmit).toHaveBeenCalled();
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.onChangeText('radioh'));
    expect(search.onChangeText).toHaveBeenCalledWith('radioh');
    expect(result.current.showSuggestions).toBe(true);

    act(() => result.current.onSuggestionSelect('radiohead'));
    expect(search.setQuery).toHaveBeenCalledWith('radiohead');
    expect(result.current.showSuggestions).toBe(false);
  });
});
