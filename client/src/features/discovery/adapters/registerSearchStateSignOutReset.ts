import { onSignOut } from '@shared/session/signOutCleanup';

import { resetSearchState } from '@features/discovery/domain/searchState';

export function registerSearchStateSignOutReset(): () => void {
  return onSignOut(resetSearchState);
}
