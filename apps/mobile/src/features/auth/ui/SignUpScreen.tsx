import type { ReactElement } from 'react';

import { useSignUp } from '../hooks/useSignUp';
import { AuthForm } from './AuthForm';

export function SignUpScreen(): ReactElement {
  const { state, signUp } = useSignUp();

  return (
    <AuthForm
      screenTestID="sign-up-screen"
      title="Create your account"
      submitLabel="Sign up"
      onSubmit={(email, password) => void signUp(email, password)}
      pending={state.kind === 'pending'}
      hasError={state.kind === 'error'}
      errorText="Sign up failed. Check your details and try again."
      linkHref="/sign-in"
      linkTestID="link-to-sign-in"
      linkText="Have an account? Sign in"
    />
  );
}
