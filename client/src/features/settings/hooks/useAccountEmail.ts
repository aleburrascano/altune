import { useSession, type SessionState } from '@shared/auth';

export function accountEmail(sessionState: SessionState): string {
  return sessionState.status === 'signed-in' ? (sessionState.session.user.email ?? '') : '';
}

export function useAccountEmail(): string {
  return accountEmail(useSession());
}
