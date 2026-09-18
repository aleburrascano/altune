import type { ReactElement } from 'react';

import { TextField } from '@shared/ui/primitives/TextField';

type NewPasswordFieldProps = {
  testID: string;
  placeholder: string;
  value: string;
  onChangeText: (text: string) => void;
  error: boolean;
};

// The four fixed props are what tells iOS/Android to offer a generated password
// and to keep it out of the autofill entry for the existing one. They only work
// when every new-password field agrees, so they live here rather than at each site.
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
