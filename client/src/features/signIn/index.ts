import { AuthCallbackScreen } from '@features/signIn/ui/AuthCallbackScreen';
import { useAuthDeepLink } from '@features/signIn/shell/useAuthDeepLink';

export { AuthCallbackScreen, useAuthDeepLink };
export { AuthGate } from '@features/signIn/ui/AuthGate';
export { ForgotPasswordScreen } from '@features/signIn/view/ForgotPasswordScreen';
export { SetNewPasswordScreen } from '@features/signIn/ui/SetNewPasswordScreen';
export { SignInScreen } from '@features/signIn/ui/SignInScreen';
export { SignUpScreen } from '@features/signIn/view/SignUpScreen';
export { AuthPortProvider, useAuthPort } from '@features/signIn/view/AuthPortProvider';
export { httpAuthPort } from '@features/signIn/adapters/http/httpAuthPort';
export {
  _resetLockoutsForTest,
  clearFailedAttempts,
  isLockedOut,
  lockoutOnRepeatedFailure,
  recordFailedAttempt,
} from '@features/signIn/shell/attemptLockout';
export type { LockoutAction } from '@features/signIn/shell/attemptLockout';
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
} from '@features/signIn/core/authError';
export type { AuthErrorReason } from '@features/signIn/core/authError';
export {
  clearRecoveryUnlock,
  isRecoveryUnlocked,
  markRecoveryUnlocked,
  recoveryUnlock,
  subscribeRecoveryUnlock,
} from '@features/signIn/shell/passwordRecovery';
export { InvalidRecoveryLinkNotice, SessionExpiredNotice } from '@features/signIn/ui/AuthNotices';
export type { AuthPort } from '@features/signIn/ports/authPort';
export type { AuthLinkIntent } from '@features/signIn/core/parseAuthLink';
export { parseAuthLink } from '@features/signIn/core/parseAuthLink';
export { initialUrl, subscribeUrl } from '@features/signIn/adapters/native/linkEvents';
export { OAuthButtons } from '@features/signIn/view/OAuthButtons';
export { authRedirectUrl, schemePrefix } from '@features/signIn/core/authRedirect';
export type { AuthLinkHost } from '@features/signIn/core/authRedirect';
export { useRecoveryUnlocked } from '@features/signIn/view/useRecoveryUnlocked';
export { CheckEmailNotice } from '@features/signIn/view/CheckEmailNotice';
export { PASSWORD_REQUIREMENTS_HINT, newPasswordFormState } from '@features/signIn/core/validation';
export { useAsyncAuthAction } from '@features/signIn/shell/useAsyncAuthAction';
export { OAUTH_BROWSER_TIMEOUT_MS } from '@features/signIn/core/oauthResult';
export { useOAuth } from '@features/signIn/hooks/useOAuth';
export { useResetPassword } from '@features/signIn/hooks/useResetPassword';
export { useSignIn } from '@features/signIn/shell/useSignIn';
export { useSignUp } from '@features/signIn/hooks/useSignUp';
export { useUpdatePassword } from '@features/signIn/shell/useUpdatePassword';
export { reportSignInFailure } from '@features/signIn/adapters/http/reportSignInFailure';
