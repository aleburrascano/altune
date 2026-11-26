import type { SessionState } from '@shared/auth';

export function accountEmail(sessionState: SessionState): string {
  return sessionState.status === 'signed-in' ? (sessionState.session.user.email ?? '') : '';
}
