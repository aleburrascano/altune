import type { ReactElement } from 'react';

import { useSignIn } from '../hooks/useSignIn';
import { authErrorText } from '../lib/errorCopy';
import { AuthForm } from './AuthForm';

const GENERIC_SIGN_IN_ERROR = 'Email or password is incorrect.';

export function SignInScreen(): ReactElement {
  const { state, signIn } = useSignIn();

  return (
    <AuthForm
      screenTestID="sign-in-screen"
      title="Welcome back"
      submitLabel="Sign in"
      onSubmit={(email, password) => void signIn(email, password)}
      pending={state.kind === 'pending'}
      hasError={state.kind === 'error'}
      errorText={
        state.kind === 'error' ? authErrorText(state.reason, GENERIC_SIGN_IN_ERROR) : ''
      }
      linkHref="/sign-up"
      linkTestID="link-to-sign-up"
      linkText="No account? Sign up"
      showForgotPassword
    />
  );
}
