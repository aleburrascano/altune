import type { SupabaseAuthErrorLike } from './supabaseAuthError';

/**
 * What an auth failure may keep about its cause, and nothing else.
 *
 * Every field an auth failure carries or logs is copied out here, so the
 * credential in flight — a `token_hash`, a PKCE `code`, an access/refresh pair —
 * has one place to be excluded rather than one per call site. Redaction by
 * projection, not by omission: a field the SDK adds later cannot ride along into
 * a log just because it was on the object (#1647).
 */

/** The name and message of a thrown value, both safe to log verbatim. */
export type ThrownErrorDetail = { name: string; message: string };

/** The fields of a resolved Supabase `{ error }` that name what GoTrue refused. */
export type SupabaseErrorDetail = Pick<
  SupabaseAuthErrorLike,
  'name' | 'code' | 'status' | 'message'
>;

// A non-Error is not stringified: whatever a rejected SDK call threw is beyond
// this module's knowledge, and `String(value)` on it could spell out a request
// body that carried the credential.
export function thrownErrorDetail(err: unknown): ThrownErrorDetail {
  return err instanceof Error
    ? { name: err.name, message: err.message }
    : { name: typeof err, message: 'a non-Error value was thrown' };
}

export function supabaseErrorDetail(error: SupabaseAuthErrorLike): SupabaseErrorDetail {
  return {
    name: error.name,
    code: error.code,
    status: error.status,
    message: error.message,
  };
}
