export { AuthCallbackScreen } from '@features/signIn/ui/AuthCallbackScreen';
export { AuthGate } from '@features/signIn/ui/AuthGate';
export { ForgotPasswordScreen } from '@features/signIn/view/ForgotPasswordScreen';
export { SetNewPasswordScreen } from '@features/signIn/ui/SetNewPasswordScreen';
export { SignInScreen } from '@features/signIn/ui/SignInScreen';
export { SignUpScreen } from '@features/signIn/view/SignUpScreen';
export { TestAuthBridge } from '@features/signIn/view/TestAuthBridge';
export { useAuthDeepLink } from '@features/signIn/hooks/useAuthDeepLink';
export { AuthPortProvider, useAuthPort } from '@features/signIn/view/AuthPortProvider';
export { httpAuthPort } from '@features/signIn/adapters/http/httpAuthPort';
export {
  _resetLockoutsForTest,
  clearFailedAttempts,
  isLockedOut,
  lockoutOnRepeatedFailure,
  recordFailedAttempt,
} from '@features/signIn/domain/attemptLockout';
export type { LockoutAction } from '@features/signIn/domain/attemptLockout';
export {
  authPortWrapper,
  createAuthPortMock,
  createSupabaseAuthMock,
  runAsyncAuthHook,
} from '@features/signIn/__tests__/testUtils/authTestUtils';
export {
  _resetConsumedCredentialForTest,
  completeAuthIntent,
} from '@features/signIn/adapters/completeAuthIntent';
export {
  authErrorText,
  classifyAuthError,
  isInvalidCredentialsError,
  isSignupDisabledError,
  isUnconfirmedEmailError,
  thrownErrorDetail,
} from '@features/signIn/domain/authError';
export type { AuthErrorReason } from '@features/signIn/domain/authError';
export {
  clearRecoveryUnlock,
  isRecoveryUnlocked,
  markRecoveryUnlocked,
  subscribeRecoveryUnlock,
} from '@features/signIn/domain/passwordRecovery';
export { InvalidRecoveryLinkNotice, SessionExpiredNotice } from '@features/signIn/ui/AuthNotices';
export type { AuthPort } from '@features/signIn/ports/authPort';
export type { AuthLinkIntent } from '@features/signIn/domain/parseAuthLink';
export { parseAuthLink } from '@features/signIn/domain/parseAuthLink';
export { initialUrl, subscribeUrl } from '@features/signIn/adapters/native/linkEvents';
export { OAuthButtons } from '@features/signIn/view/OAuthButtons';
export { authRedirectUrl } from '@features/signIn/domain/authRedirect';
export { useRecoveryUnlocked } from '@features/signIn/view/useRecoveryUnlocked';
export { CheckEmailNotice } from '@features/signIn/view/CheckEmailNotice';
export {
  PASSWORD_REQUIREMENTS_HINT,
  newPasswordFormState,
} from '@features/signIn/domain/validation';
export {
  bootstrapTestAuth,
  buildTestSession,
  fetchTestLoginToken,
  isTestAuthEnabled,
} from '@features/signIn/adapters/http/testAuth';
export type { TestLoginResponse } from '@features/signIn/adapters/http/testAuth';
export { useAsyncAuthAction } from '@features/signIn/hooks/useAsyncAuthAction';
export { OAUTH_BROWSER_TIMEOUT_MS } from '@features/signIn/domain/oauthResult';
export { useOAuth } from '@features/signIn/hooks/useOAuth';
export { useResetPassword } from '@features/signIn/hooks/useResetPassword';
export { useSignIn } from '@features/signIn/hooks/useSignIn';
export { useSignUp } from '@features/signIn/hooks/useSignUp';
export { useUpdatePassword } from '@features/signIn/hooks/useUpdatePassword';
export { reportSignInFailure } from '@features/signIn/adapters/http/reportSignInFailure';
