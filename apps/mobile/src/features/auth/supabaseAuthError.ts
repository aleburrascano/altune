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
