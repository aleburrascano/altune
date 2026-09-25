import type { QueryClient } from '@tanstack/react-query';

import { runSignOutCleanups } from '@shared/session/signOutCleanup';

import { clearSessionExpired } from './sessionExpired';

export function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  runSignOutCleanups();
}
