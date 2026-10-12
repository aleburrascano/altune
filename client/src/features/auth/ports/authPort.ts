import type { SignInFailureReason } from '@features/auth/domain/authError';

export interface AuthPort {
  reportSignInFailure(reason: SignInFailureReason): void;
  isTestAuthEnabled(): boolean;
  bootstrapTestAuth(): Promise<boolean>;
}
