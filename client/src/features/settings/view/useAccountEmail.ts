import { useSession } from '@shared/auth';
import { accountEmail } from '@features/settings/core/accountEmail';

export function useAccountEmail(): string {
  return accountEmail(useSession());
}
