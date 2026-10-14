import { signUp } from '@shared/auth/authActions';

import {
  classifyAuthError,
  isAlreadyRegisteredError,
  isSignupDisabledError,
  isWeakPasswordError,
  supabaseErrorDetail,
  type TransientAuthReason,
} from '@features/auth/domain/authError';
import { authRedirectUrl } from '@features/auth/domain/authRedirect';

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

type SignUpResponseData = Awaited<ReturnType<typeof signUp>>['data'];

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
      const { data: response, error } = await signUp({
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
      return signUpOutcome(response);
    },
  );

  return { state, signUp: run };
}
