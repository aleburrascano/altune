import type { SupabaseClient } from '@supabase/supabase-js';

type Auth = SupabaseClient['auth'];

type PasswordChange = { password: string };

export type AuthClient = {
  readonly auth: Auth;
  clearPersistedSession(): Promise<void>;
};

let installed: AuthClient | undefined;

export function installAuthClient(client: AuthClient): void {
  installed = client;
}

function authClient(): AuthClient {
  if (installed === undefined) {
    throw new Error('auth has no client: the app installs one at startup, a test passes in a fake');
  }
  return installed;
}

export const supabase: { readonly auth: Auth } = {
  get auth() {
    return authClient().auth;
  },
};

export function clearPersistedAuthSession(): Promise<void> {
  return authClient().clearPersistedSession();
}

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
