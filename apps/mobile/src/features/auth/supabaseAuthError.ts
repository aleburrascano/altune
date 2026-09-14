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

/**
 * A transient transport failure the SDK swallowed and resolved with: the request
 * never reached GoTrue (offline / DNS: `status` 0), the server was unreachable
 * (5xx), or we were rate-limited (429). `AuthRetryableFetchError` is the SDK's
 * own name for this class of failure.
 */
export function isTransportAuthError(error: SupabaseAuthErrorLike): boolean {
  return (
    error.name === 'AuthRetryableFetchError' ||
    error.status === 0 ||
    error.status === 429 ||
    (typeof error.status === 'number' && error.status >= 500)
  );
}

/** Supabase judged the supplied password too weak (signUp / updateUser). */
export function isWeakPasswordError(error: SupabaseAuthErrorLike): boolean {
  return error.name === 'AuthWeakPasswordError' || error.code === 'weak_password';
}

/** The email is already registered to a confirmed account (signUp). */
export function isAlreadyRegisteredError(error: SupabaseAuthErrorLike): boolean {
  return error.code === 'user_already_exists' || error.code === 'email_exists';
}
