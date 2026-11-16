import { onSignOut } from '@shared/session';

import { resetSearchState } from '@features/discovery/core/searchState';

export function registerSearchStateSignOutReset(): () => void {
  return onSignOut(resetSearchState);
}
