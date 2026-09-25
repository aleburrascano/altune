import type { ReactElement } from 'react';

import { TextField } from '@shared/ui/primitives/TextField';

import { PASSWORD_REQUIREMENTS_HINT } from '../validation';
import { FieldError } from './FieldError';
import { NewPasswordField } from './NewPasswordField';

type EmailPasswordFieldsProps = {
  email: string;
  onChangeEmail: (text: string) => void;
  showEmailError: boolean;
  password: string;
  onChangePassword: (text: string) => void;
  // Which password the OS should offer: the one it has saved, or a generated new
  // one. The two autofill contracts are mutually exclusive on iOS and Android.
  passwordKind: 'existing' | 'new';
  showPasswordError: boolean;
};

export function EmailPasswordFields({
  email,
  onChangeEmail,
  showEmailError,
  password,
  onChangePassword,
  passwordKind,
  showPasswordError,
}: EmailPasswordFieldsProps): ReactElement {
  return (
    <>
      <TextField
        testID="email-input"
        value={email}
        onChangeText={onChangeEmail}
        placeholder="Email"
        autoCapitalize="none"
        autoCorrect={false}
        keyboardType="email-address"
        textContentType="emailAddress"
        autoComplete="email"
        error={showEmailError}
      />
      {showEmailError ? (
        <FieldError testID="email-error">Enter a valid email address.</FieldError>
      ) : null}
      {passwordKind === 'new' ? (
        <NewPasswordField
          testID="password-input"
          value={password}
          onChangeText={onChangePassword}
          placeholder="Password"
          error={showPasswordError}
        />
      ) : (
        <TextField
          testID="password-input"
          value={password}
          onChangeText={onChangePassword}
          placeholder="Password"
          secure
          autoCapitalize="none"
          textContentType="password"
          autoComplete="password"
          error={showPasswordError}
        />
      )}
      {showPasswordError ? (
        <FieldError testID="password-error">{PASSWORD_REQUIREMENTS_HINT}</FieldError>
      ) : null}
    </>
  );
}
