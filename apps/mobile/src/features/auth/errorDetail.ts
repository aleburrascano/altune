import type { SupabaseAuthErrorLike } from './supabaseAuthError';

export type ThrownErrorDetail = { name: string; message: string };

export type SupabaseErrorDetail = Pick<
  SupabaseAuthErrorLike,
  'name' | 'code' | 'status' | 'message'
>;

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
