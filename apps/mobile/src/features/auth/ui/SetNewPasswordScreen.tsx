import { useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { TextField } from '@shared/ui/primitives/TextField';
import { spacing } from '@shared/ui/theme';

import { useUpdatePassword } from '../hooks/useUpdatePassword';
import { authErrorText } from '../lib/errorCopy';
import { PASSWORD_REQUIREMENTS_HINT, passwordsMatch, validatePassword } from '../lib/validation';
import { AuthHeroLayout } from './hero/AuthHeroLayout';

const GENERIC_ERROR = "Couldn't update your password. Please try again.";

export function SetNewPasswordScreen(): ReactElement {
  const router = useRouter();
  const { state, updatePassword } = useUpdatePassword();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');

  const passwordIssues = validatePassword(password);
  const matches = passwordsMatch(password, confirm);
  const formValid = passwordIssues.length === 0 && matches && password.length > 0;

  useEffect(() => {
    if (state.kind === 'ok') {
      router.replace('/library');
    }
  }, [state.kind, router]);

  return (
    <AuthHeroLayout testID="set-new-password-screen">
      <View style={styles.form}>
        <Text variant="title">Choose a new password</Text>
        <TextField
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="New password"
          secure
          autoCapitalize="none"
          textContentType="newPassword"
          autoComplete="new-password"
          error={passwordIssues.length > 0 && password.length > 0}
        />
        <TextField
          testID="confirm-input"
          value={confirm}
          onChangeText={setConfirm}
          placeholder="Confirm new password"
          secure
          autoCapitalize="none"
          textContentType="newPassword"
          autoComplete="new-password"
          error={confirm.length > 0 && !matches}
        />
        {passwordIssues.length > 0 && password.length > 0 ? (
          <Text testID="password-error" variant="caption" tone="danger">
            {PASSWORD_REQUIREMENTS_HINT}
          </Text>
        ) : null}
        {confirm.length > 0 && !matches ? (
          <Text testID="confirm-error" variant="caption" tone="danger">
            Passwords don&apos;t match.
          </Text>
        ) : null}
        <Button
          testID="submit-button"
          label="Update password"
          onPress={() => void updatePassword(password)}
          loading={state.kind === 'pending'}
          disabled={!formValid}
        />
        {state.kind === 'error' ? (
          <Banner testID="auth-error" tone="danger">
            {authErrorText(state.reason, GENERIC_ERROR)}
          </Banner>
        ) : null}
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.sm },
});
