import { supabase } from '@shared/auth/adapters/authActions';
import { primeSignedInState } from '@shared/session';

export async function primeSignedInUser(): Promise<void> {
  try {
    const { data: sessionData } = await supabase.auth.getSession();
    primeSignedInState(Boolean(sessionData.session));
  } catch (error) {
    console.warn('[auth] headless sign-in prime failed', error);
  }
}
