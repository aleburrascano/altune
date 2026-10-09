import type { ReactElement } from 'react';

import { TextField } from '@shared/ui/primitives/TextField';

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
