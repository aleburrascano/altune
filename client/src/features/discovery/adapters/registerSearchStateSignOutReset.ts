import { onSignOut } from '@shared/session';

import { resetSearchState } from '@features/discovery/domain/searchState';

export function registerSearchStateSignOutReset(): () => void {
  return onSignOut(resetSearchState);
}
