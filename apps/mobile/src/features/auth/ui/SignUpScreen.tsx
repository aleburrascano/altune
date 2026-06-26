import type { ReactElement } from 'react';

import { useSignUp } from '../hooks/useSignUp';
import { authErrorText } from '../lib/errorCopy';
import { AuthForm } from './AuthForm';
import { CheckEmailNotice } from './CheckEmailNotice';

const GENERIC_SIGN_UP_ERROR = "Couldn't create your account. Please try again.";

export function SignUpScreen(): ReactElement {
  const { state, signUp } = useSignUp();

  if (state.kind === 'awaiting-confirmation') {
    return <CheckEmailNotice />;
  }

  return (
    <AuthForm
      screenTestID="sign-up-screen"
      tagline="Every track you own, in one place."
      submitLabel="Sign up"
      onSubmit={(email, password) => void signUp(email, password)}
      pending={state.kind === 'pending'}
      hasError={state.kind === 'error'}
      errorText={
        state.kind === 'error' ? authErrorText(state.reason, GENERIC_SIGN_UP_ERROR) : ''
      }
      linkHref="/sign-in"
      linkTestID="link-to-sign-in"
      linkQuestion="Have an account?"
      linkAction="Sign in"
      showConfirm
      enforcePasswordPolicy
    />
  );
}
