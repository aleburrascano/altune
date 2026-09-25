import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { getSearchState, setSearchState } from '../search-state';

describe('search-state registration with the sign-out registry', () => {
  it('is reset when the sign-out cleanups run', () => {
    setSearchState('radiohead', 'radiohead');

    runSignOutCleanups();

    expect(getSearchState()).toEqual({ query: '', inputValue: '' });
  });
});
