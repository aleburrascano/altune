import { supabase } from '@shared/auth/supabaseClient';
import { primeSignedInState } from '@shared/session/signOutCleanup';

export async function primeSignedInUser(): Promise<void> {
  try {
    const { data: sessionData } = await supabase.auth.getSession();
    primeSignedInState(Boolean(sessionData.session));
  } catch (error) {
    console.warn('[auth] headless sign-in prime failed', error);
  }
}
