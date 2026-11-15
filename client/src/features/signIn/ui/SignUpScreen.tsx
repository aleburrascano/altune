import { useState, type ReactElement } from 'react';

import { useEmailPasswordFields } from '@features/signIn/hooks/useEmailPasswordFields';
import { useSignUp } from '@features/signIn/hooks/useSignUp';
import { newPasswordFormState } from '@features/signIn/domain/validation';
import { AuthForm } from '@features/signIn/ui/AuthForm';
import { CheckEmailNotice } from '@features/signIn/ui/CheckEmailNotice';
import { EmailPasswordFields, FieldError, NewPasswordField } from '@features/signIn/ui/AuthFields';

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
        <FieldError testID="confirm-error">Passwords don&apos;t match.</FieldError>
      ) : null}
    </AuthForm>
  );
}
