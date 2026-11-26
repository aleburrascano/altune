export {
  authIntentClient,
  installAuthClient,
  requestPasswordReset,
  signInWithPassword,
  signOutOtherSessions,
  signUp,
  startOAuth,
  supabase,
  updatePassword,
} from '@shared/auth/adapters/authActions';
export type { AuthClient } from '@shared/auth/adapters/authActions';
export { withAuthDeadline, withinAuthDeadline } from '@shared/auth/core/authDeadline';
export { primeSignedInUser } from '@shared/auth/shell/primeSignedInUser';
export {
  markSessionExpired,
  stampCredentials,
  clearSessionExpired,
  getSessionExpired,
  renewSessionCredentials,
  subscribeSessionExpired,
} from '@shared/auth/core/sessionExpired';
export type { CredentialStamp } from '@shared/auth/core/sessionExpired';
export { useSessionExpired } from '@shared/auth/view/sessionExpired';
export { useSession } from '@shared/auth/view/useSession';
export type { SessionState } from '@shared/auth/view/useSession';
export { useSignOut } from '@shared/auth/view/useSignOut';
export type { SignOutResult } from '@shared/auth/view/useSignOut';
