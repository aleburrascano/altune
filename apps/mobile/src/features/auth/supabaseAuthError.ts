import type { AuthErrorReason } from './errorReason';

export type SupabaseAuthErrorLike = {
  name?: string | undefined;
  code?: string | undefined;
  status?: number | undefined;
  message?: string | undefined;
};

export function isTransportAuthError(error: SupabaseAuthErrorLike): boolean {
  return (
    error.name === 'AuthRetryableFetchError' ||
    error.status === 0 ||
    (typeof error.status === 'number' && error.status >= 500)
  );
}

export function isRateLimitedAuthError(error: SupabaseAuthErrorLike): boolean {
  return (
    error.status === 429 ||
    (typeof error.code === 'string' && /^over_.*_rate_limit$/.test(error.code))
  );
}

export function isInvalidCredentialsError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'invalid_credentials';
}

export function isUnconfirmedEmailError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'email_not_confirmed';
}

export function isWeakPasswordError(error: SupabaseAuthErrorLike): boolean {
  return error.name === 'AuthWeakPasswordError' || error.code === 'weak_password';
}

export function isAlreadyRegisteredError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'user_already_exists' || error.code === 'email_exists';
}

type Rung<R extends AuthErrorReason> = readonly [(error: SupabaseAuthErrorLike) => boolean, R];

export function classifyAuthError<R extends AuthErrorReason = never>(
  error: SupabaseAuthErrorLike,
  rungs: readonly Rung<R>[] = [],
): 'too_many_attempts' | 'network' | R | 'unknown' {
  if (isRateLimitedAuthError(error)) return 'too_many_attempts';
  if (isTransportAuthError(error)) return 'network';
  return rungs.find(([matches]) => matches(error))?.[1] ?? 'unknown';
}
