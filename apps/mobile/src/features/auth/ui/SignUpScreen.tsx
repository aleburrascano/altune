import { useState, type ReactElement } from 'react';

import { Text } from '@shared/ui/primitives/Text';

import { useEmailPasswordFields } from '../hooks/useEmailPasswordFields';
import { useSignUp } from '../hooks/useSignUp';
import { newPasswordFormState } from '../validation';
import { AuthForm } from './AuthForm';
import { CheckEmailNotice } from './CheckEmailNotice';
import { EmailPasswordFields } from './EmailPasswordFields';
import { NewPasswordField } from './NewPasswordField';

const GENERIC_SIGN_UP_ERROR = "Couldn't create your account. Please try again.";

export function SignUpScreen(): ReactElement {
  const { state, signUp } = useSignUp();
  const fields = useEmailPasswordFields();
  const [confirm, setConfirm] = useState('');

  const { showPasswordError, showConfirmError, valid } = newPasswordFormState(
    fields.password,
    confirm,
  );
  const canSubmit = fields.emailValid && valid;

  if (state.kind === 'awaiting-confirmation') {
    return <CheckEmailNotice />;
  }

  return (
    <AuthForm
      screenTestID="sign-up-screen"
      tagline="Every track you own, in one place."
      submitLabel="Sign up"
      onSubmit={() => void signUp(fields.credentials.email, fields.credentials.password)}
      pending={state.kind === 'pending'}
      canSubmit={canSubmit}
      state={state}
      generic={GENERIC_SIGN_UP_ERROR}
      linkHref="/sign-in"
      linkTestID="link-to-sign-in"
      linkQuestion="Have an account?"
      linkAction="Sign in"
    >
      <EmailPasswordFields
        email={fields.email}
        onChangeEmail={fields.setEmail}
        showEmailError={fields.showEmailError}
        password={fields.password}
        onChangePassword={fields.setPassword}
        passwordKind="new"
        showPasswordError={showPasswordError}
      />
      <NewPasswordField
        testID="confirm-input"
        value={confirm}
        onChangeText={setConfirm}
        placeholder="Confirm password"
        error={showConfirmError}
      />
      {showConfirmError ? (
        <Text testID="confirm-error" variant="caption" tone="danger">
          Passwords don&apos;t match.
        </Text>
      ) : null}
    </AuthForm>
  );
}
