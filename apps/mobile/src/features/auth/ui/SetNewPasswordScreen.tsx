import { useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { showFailureAlert } from '@shared/ui/failure/showFailure';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { useUpdatePassword } from '@features/auth/hooks/useUpdatePassword';
import { clearRecoveryUnlock } from '@features/auth/recoveryUnlock';
import { PASSWORD_REQUIREMENTS_HINT, newPasswordFormState } from '@features/auth/validation';
import { AuthErrorBanner } from '@features/auth/ui/AuthErrorBanner';
import { AuthHeroLayout } from '@features/auth/ui/hero/AuthHeroLayout';
import { FieldError } from '@features/auth/ui/FieldError';
import { NewPasswordField } from '@features/auth/ui/NewPasswordField';

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
      if (!state.othersRevoked) {
        showFailureAlert({
          surface: 'auth.password.others_not_revoked',
          title: OTHERS_NOT_REVOKED_TITLE,
          message: OTHERS_NOT_REVOKED_MESSAGE,
        });
      }
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
          action="auth.set_new_password.submit"
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
