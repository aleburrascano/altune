/**
 * Structural classification of a Supabase auth error.
 *
 * `signInWithPassword` / `signUp` / `updateUser` / `resetPasswordForEmail` catch
 * transport failures internally and *resolve* with `{ error }` (an
 * `AuthRetryableFetchError`) rather than throwing — so the only reliable way to
 * tell a network outage from a real auth rejection is to inspect the resolved
 * error's `name` / `status` / `code`. We match by shape (not `instanceof`) so
 * detection works for real SDK errors and resolved-`{ error }` fixtures alike.
 */
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

/**
 * GoTrue judged the address/password pair itself wrong (signInWithPassword).
 * Recognised positively, by code: a rejection we cannot name is a rejection of
 * the request, not a verdict on the password, and must not be reported as one
 * (#1646).
 */
export function isInvalidCredentialsError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'invalid_credentials';
}

/** The password was right; GoTrue withholds the session until the address is confirmed. */
export function isUnconfirmedEmailError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'email_not_confirmed';
}

/** Supabase judged the supplied password too weak (signUp / updateUser). */
export function isWeakPasswordError(error: SupabaseAuthErrorLike): boolean {
  return error.name === 'AuthWeakPasswordError' || error.code === 'weak_password';
}

/** The email is already registered to a confirmed account (signUp). */
export function isAlreadyRegisteredError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'user_already_exists' || error.code === 'email_exists';
}
