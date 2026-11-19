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
export { withAuthDeadline, withinAuthDeadline } from '@shared/auth/shell/authDeadline';
export { primeSignedInUser } from '@shared/auth/shell/primeSignedInUser';
export {
  markSessionExpired,
  stampCredentials,
  useSessionExpired,
  clearSessionExpired,
  getSessionExpired,
  renewSessionCredentials,
} from '@shared/auth/view/sessionExpired';
export type { CredentialStamp } from '@shared/auth/view/sessionExpired';
export { useSession } from '@shared/auth/useSession';
export type { SessionState } from '@shared/auth/useSession';
export { useSignOut } from '@shared/auth/useSignOut';
export type { SignOutResult } from '@shared/auth/useSignOut';
