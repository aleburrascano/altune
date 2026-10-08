import { supabase } from '@shared/auth/supabaseClient';

import { supabaseErrorDetail } from '@features/auth/domain/errorDetail';
import type { TransientAuthReason } from '@features/auth/domain/errorReason';
import { authRedirectUrl } from '@features/auth/domain/authRedirect';
import {
  classifyAuthError,
  isAlreadyRegisteredError,
  isSignupDisabledError,
  isWeakPasswordError,
} from '@features/auth/domain/supabaseAuthError';

import { useAsyncAuthAction } from '@features/auth/hooks/useAsyncAuthAction';

type SignUpResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'awaiting-confirmation' }
  | {
      kind: 'error';
      reason: TransientAuthReason | 'weak_password' | 'already_registered' | 'signup_disabled';
    };

type SettledSignUp = Exclude<SignUpResult, { kind: 'idle' | 'pending' }>;

type SignUpResponseData = { user?: { identities?: unknown } | null; session?: unknown } | null;

function signUpOutcome(data: SignUpResponseData): SettledSignUp {
  if (data?.session) return { kind: 'ok' };
  const identities = data?.user?.identities;
  if (!Array.isArray(identities)) {
    console.warn('[auth] sign-up response carried no identities list', {
      identitiesType: typeof identities,
    });
    return { kind: 'error', reason: 'unknown' };
  }
  return identities.length === 0
    ? { kind: 'error', reason: 'already_registered' }
    : { kind: 'awaiting-confirmation' };
}

export function useSignUp() {
  const { state, run } = useAsyncAuthAction<SignUpResult, [string, string]>(
    async (email, password) => {
      const { data, error } = await supabase.auth.signUp({
        email,
        password,
        options: { emailRedirectTo: authRedirectUrl('confirm') },
      });
      if (error) {
        console.warn('[auth] sign-up failed', supabaseErrorDetail(error));
        return {
          kind: 'error',
          reason: classifyAuthError(error, [
            [isWeakPasswordError, 'weak_password'],
            [isAlreadyRegisteredError, 'already_registered'],
            [isSignupDisabledError, 'signup_disabled'],
          ]),
        };
      }
      return signUpOutcome(data);
    },
  );

  return { state, signUp: run };
}
