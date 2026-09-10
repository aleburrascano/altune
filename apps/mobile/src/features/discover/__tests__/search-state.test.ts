import { getSearchState, setSearchState } from '../search-state';
import type * as SearchStateModule from '../search-state';

beforeEach(() => {
  setSearchState('', '');
});

describe('search-state preserves the last query across a detail round trip', () => {
  it('reads back exactly what was written', () => {
    setSearchState('radiohead', 'radioh');

    expect(getSearchState()).toEqual({ query: 'radiohead', inputValue: 'radioh' });
  });

  it('reflects the most recent write, not a stale snapshot', () => {
    setSearchState('first', 'fir');
    setSearchState('second', 'sec');

    expect(getSearchState()).toEqual({ query: 'second', inputValue: 'sec' });
  });

  it('keeps the committed query and the input value as independent fields', () => {
    setSearchState('committed value', 'typed value');

    const state = getSearchState();

    expect(state.query).toBe('committed value');
    expect(state.inputValue).toBe('typed value');
  });

  it('defaults both fields to empty strings before anything has been written', () => {
    jest.isolateModules(() => {
      const fresh: typeof SearchStateModule = require('../search-state');

      expect(fresh.getSearchState()).toEqual({ query: '', inputValue: '' });
    });
  });
});
