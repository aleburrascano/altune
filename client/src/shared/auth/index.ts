export {
  authIntentClient,
  requestPasswordReset,
  signInWithPassword,
  signOutOtherSessions,
  signUp,
  startOAuth,
  updatePassword,
} from '@shared/auth/authActions';
export { withAuthDeadline, withinAuthDeadline } from '@shared/auth/authDeadline';
export { injectTestSession } from '@shared/auth/injectTestSession';
export { primeSignedInUser } from '@shared/auth/primeSignedInUser';
export {
  markSessionExpired,
  stampCredentials,
  useSessionExpired,
} from '@shared/auth/sessionExpired';
export type { CredentialStamp } from '@shared/auth/sessionExpired';
export { supabase } from '@shared/auth/supabaseClient';
export { isTestAuthEnabled, markTestAuthBootSettled } from '@shared/auth/testAuthBoot';
export { useSession } from '@shared/auth/useSession';
export type { SessionState } from '@shared/auth/useSession';
export { useSignOut } from '@shared/auth/useSignOut';
export type { SignOutResult } from '@shared/auth/useSignOut';
