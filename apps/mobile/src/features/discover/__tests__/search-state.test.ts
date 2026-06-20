import { getSearchState, setSearchState } from '../search-state';

describe('search-state', () => {
  it('returns empty defaults before any set', () => {
    const state = getSearchState();
    expect(state.query).toBe('');
    expect(state.inputValue).toBe('');
  });

  it('roundtrips a set/get', () => {
    setSearchState('radiohead', 'radioh');
    const state = getSearchState();
    expect(state.query).toBe('radiohead');
    expect(state.inputValue).toBe('radioh');
  });

  it('overwrites previous state on subsequent set', () => {
    setSearchState('first', 'f');
    setSearchState('second', 's');
    const state = getSearchState();
    expect(state.query).toBe('second');
    expect(state.inputValue).toBe('s');
  });
});
