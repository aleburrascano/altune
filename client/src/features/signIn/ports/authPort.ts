import type { SignInFailureReason } from '@features/signIn/core/authError';

export interface AuthPort {
  reportSignInFailure(reason: SignInFailureReason): void;
  isTestAuthEnabled(): boolean;
  bootstrapTestAuth(): Promise<boolean>;
}
