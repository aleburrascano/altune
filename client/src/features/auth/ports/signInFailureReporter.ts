import type { SignInFailureReason } from '@features/auth/domain/errorReason';

export type SignInFailureReporter = (reason: SignInFailureReason) => void;
