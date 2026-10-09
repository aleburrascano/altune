import type { ReactElement, ReactNode } from 'react';

import { Text } from '@shared/ui/primitives/Text';
import { TextField } from '@shared/ui/primitives/TextField';

import { PASSWORD_REQUIREMENTS_HINT } from '@features/auth/domain/validation';

type FieldErrorProps = {
  testID: string;
  children: ReactNode;
};

export function FieldError({ testID, children }: FieldErrorProps): ReactElement {
  return (
    <Text testID={testID} variant="caption" tone="danger">
      {children}
    </Text>
  );
}

type NewPasswordFieldProps = {
  testID: string;
  placeholder: string;
  value: string;
  onChangeText: (text: string) => void;
  error: boolean;
};

export function NewPasswordField({
  testID,
  placeholder,
  value,
  onChangeText,
  error,
}: NewPasswordFieldProps): ReactElement {
  return (
    <TextField
      testID={testID}
      value={value}
      onChangeText={onChangeText}
      placeholder={placeholder}
      secure
      autoCapitalize="none"
      textContentType="newPassword"
      autoComplete="new-password"
      error={error}
    />
  );
}

type EmailPasswordFieldsProps = {
  email: string;
  onChangeEmail: (text: string) => void;
  showEmailError: boolean;
  password: string;
  onChangePassword: (text: string) => void;
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
