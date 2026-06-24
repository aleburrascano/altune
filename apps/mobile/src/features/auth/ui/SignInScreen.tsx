import type { ReactElement } from 'react';

import { useSignIn } from '../hooks/useSignIn';
import { AuthForm } from './AuthForm';

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
      errorText="Sign in failed. Check your details and try again."
      linkHref="/sign-up"
      linkTestID="link-to-sign-up"
      linkText="No account? Sign up"
    />
  );
}
