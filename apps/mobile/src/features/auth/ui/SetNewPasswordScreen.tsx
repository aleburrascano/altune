import { useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { showAlert } from '@shared/ui/dialog/dialog';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { useUpdatePassword } from '../hooks/useUpdatePassword';
import { clearRecoveryUnlock } from '../recoveryUnlock';
import { PASSWORD_REQUIREMENTS_HINT, newPasswordFormState } from '../validation';
import { AuthErrorBanner } from './AuthErrorBanner';
import { AuthHeroLayout } from './hero/AuthHeroLayout';
import { FieldError } from './FieldError';
import { NewPasswordField } from './NewPasswordField';

const OTHERS_NOT_REVOKED_TITLE = 'Password updated';
const OTHERS_NOT_REVOKED_MESSAGE =
  "We couldn't sign out your other devices. They may still be signed in.";
const GENERIC_ERROR = "Couldn't update your password. Please try again.";

export function SetNewPasswordScreen(): ReactElement {
  const router = useRouter();
  const { state, updatePassword } = useUpdatePassword();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');

  const { showPasswordError, showConfirmError, valid } = newPasswordFormState(password, confirm);

  useEffect(() => {
    if (state.kind === 'ok') {
      if (!state.othersRevoked) showAlert(OTHERS_NOT_REVOKED_TITLE, OTHERS_NOT_REVOKED_MESSAGE);
      clearRecoveryUnlock();
      router.replace('/library');
    }
  }, [state, router]);

  return (
    <AuthHeroLayout testID="set-new-password-screen">
      <View style={styles.form}>
        <Text variant="title">Choose a new password</Text>
        <NewPasswordField
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="New password"
          error={showPasswordError}
        />
        <NewPasswordField
          testID="confirm-input"
          value={confirm}
          onChangeText={setConfirm}
          placeholder="Confirm new password"
          error={showConfirmError}
        />
        {showPasswordError ? (
          <FieldError testID="password-error">{PASSWORD_REQUIREMENTS_HINT}</FieldError>
        ) : null}
        {showConfirmError ? (
          <FieldError testID="confirm-error">Passwords don&apos;t match.</FieldError>
        ) : null}
        <Button
          testID="submit-button"
          label="Update password"
          onPress={() => void updatePassword(password)}
          loading={state.kind === 'pending'}
          disabled={!valid}
        />
        <AuthErrorBanner state={state} generic={GENERIC_ERROR} />
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.sm },
});
