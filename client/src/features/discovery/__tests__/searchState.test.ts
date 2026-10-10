import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import {
  getSearchState,
  resetSearchState,
  setSearchState,
} from '@features/discovery/domain/searchState';
import { registerSearchStateSignOutReset } from '@features/discovery/adapters/registerSearchStateSignOutReset';
import type * as SearchStateModule from '@features/discovery/domain/searchState';

beforeEach(() => {
  setSearchState('', '');
});

describe('searchState preserves the last query across a detail round trip', () => {
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

  it('resetSearchState returns both fields to empty strings', () => {
    setSearchState('committed value', 'typed value');

    resetSearchState();

    expect(getSearchState()).toEqual({ query: '', inputValue: '' });
  });

  it('defaults both fields to empty strings before anything has been written', () => {
    jest.isolateModules(() => {
      const fresh: typeof SearchStateModule = require('@features/discovery/domain/searchState');

      expect(fresh.getSearchState()).toEqual({ query: '', inputValue: '' });
    });
  });
});

describe('searchState registration with the sign-out registry', () => {
  it('is reset when the sign-out cleanups run once registered', () => {
    const unregister = registerSearchStateSignOutReset();
    setSearchState('radiohead', 'radiohead');

    runSignOutCleanups();
    unregister();

    expect(getSearchState()).toEqual({ query: '', inputValue: '' });
  });

  it('survives the sign-out cleanups while nothing registered it', () => {
    setSearchState('radiohead', 'radiohead');

    runSignOutCleanups();

    expect(getSearchState()).toEqual({ query: 'radiohead', inputValue: 'radiohead' });
  });
});
