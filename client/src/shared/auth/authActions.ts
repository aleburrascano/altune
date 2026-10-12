import type { SupabaseClient } from '@supabase/supabase-js';

import { supabase } from '@shared/auth/supabaseClient';

type Auth = SupabaseClient['auth'];

type PasswordChange = { password: string };

export function signInWithPassword(...args: Parameters<Auth['signInWithPassword']>) {
  return supabase.auth.signInWithPassword(...args);
}

export function signUp(...args: Parameters<Auth['signUp']>) {
  return supabase.auth.signUp(...args);
}

export function requestPasswordReset(...args: Parameters<Auth['resetPasswordForEmail']>) {
  return supabase.auth.resetPasswordForEmail(...args);
}

export function updatePassword(attributes: PasswordChange) {
  return supabase.auth.updateUser(attributes);
}

export function signOutOtherSessions() {
  return supabase.auth.signOut({ scope: 'others' });
}

export function startOAuth(...args: Parameters<Auth['signInWithOAuth']>) {
  return supabase.auth.signInWithOAuth(...args);
}

export function authIntentClient(): Pick<
  Auth,
  'exchangeCodeForSession' | 'verifyOtp' | 'getSession'
> {
  return supabase.auth;
}
