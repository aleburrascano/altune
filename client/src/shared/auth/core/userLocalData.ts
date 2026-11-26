import type { QueryClient } from '@tanstack/react-query';

import { runSignOutCleanups } from '@shared/session';

import { clearSessionExpired } from '@shared/auth/core/sessionExpired';

export function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  runSignOutCleanups();
}
