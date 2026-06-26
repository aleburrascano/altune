import { useRouter } from 'expo-router';
import { useEffect, useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useUpdatePassword } from '../hooks/useUpdatePassword';
import { authErrorText } from '../lib/errorCopy';
import { PASSWORD_REQUIREMENTS_HINT, passwordsMatch, validatePassword } from '../lib/validation';

const GENERIC_ERROR = "Couldn't update your password. Please try again.";

export function SetNewPasswordScreen(): ReactElement {
  const theme = useTheme();
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

  const fieldColors = {
    borderColor: theme.color.border,
    backgroundColor: theme.color.surface1,
    color: theme.color.textPrimary,
  };

  return (
    <Screen testID="set-new-password-screen">
      <View style={styles.body}>
        <View style={styles.header}>
          <Wordmark size={40} />
          <Text variant="title">Choose a new password</Text>
        </View>
        <TextInput
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="New password"
          placeholderTextColor={theme.color.textTertiary}
          secureTextEntry
          style={[styles.input, fieldColors]}
        />
        {passwordIssues.length > 0 && password.length > 0 ? (
          <Text testID="password-error" variant="caption" tone="danger">
            {PASSWORD_REQUIREMENTS_HINT}
          </Text>
        ) : null}
        <TextInput
          testID="confirm-input"
          value={confirm}
          onChangeText={setConfirm}
          placeholder="Confirm new password"
          placeholderTextColor={theme.color.textTertiary}
          secureTextEntry
          style={[styles.input, fieldColors]}
        />
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
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1, justifyContent: 'center', gap: spacing.md },
  header: { alignItems: 'center', gap: spacing.sm, marginBottom: spacing.xl },
  input: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
});
