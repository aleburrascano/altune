import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useResetPassword } from '../hooks/useResetPassword';
import { authErrorText } from '../lib/errorCopy';
import { isValidEmail } from '../lib/validation';

const GENERIC_ERROR = "Couldn't send the reset email. Please try again.";
// Anti-enumeration: identical message whether or not the address has an account.
const SENT_COPY = "If an account exists for that email, we've sent a reset link.";

export function ForgotPasswordScreen(): ReactElement {
  const theme = useTheme();
  const router = useRouter();
  const { state, requestReset } = useResetPassword();
  const [email, setEmail] = useState('');

  const emailValid = isValidEmail(email);
  const showEmailError = email.length > 0 && !emailValid;
  const sent = state.kind === 'sent';

  return (
    <Screen testID="forgot-password-screen">
      <View style={styles.body}>
        <View style={styles.header}>
          <Wordmark size={40} />
          <Text variant="title">Reset your password</Text>
        </View>

        {sent ? (
          <Banner testID="reset-sent" tone="info">
            {SENT_COPY} Check your email and follow the link to choose a new password.
          </Banner>
        ) : (
          <>
            <Text variant="label" tone="secondary">
              Enter your email and we&apos;ll send you a link to reset your password.
            </Text>
            <TextInput
              testID="email-input"
              value={email}
              onChangeText={setEmail}
              placeholder="Email"
              placeholderTextColor={theme.color.textTertiary}
              autoCapitalize="none"
              keyboardType="email-address"
              style={[
                styles.input,
                {
                  borderColor: theme.color.border,
                  backgroundColor: theme.color.surface1,
                  color: theme.color.textPrimary,
                },
              ]}
            />
            {showEmailError ? (
              <Text testID="email-error" variant="caption" tone="danger">
                Enter a valid email address.
              </Text>
            ) : null}
            <Button
              testID="submit-button"
              label="Send reset link"
              onPress={() => void requestReset(email)}
              loading={state.kind === 'pending'}
              disabled={!emailValid}
            />
            {state.kind === 'error' ? (
              <Banner testID="auth-error" tone="danger">
                {authErrorText(state.reason, GENERIC_ERROR)}
              </Banner>
            ) : null}
          </>
        )}

        <View style={styles.linkWrap}>
          <Text
            testID="back-to-sign-in"
            variant="label"
            tone="accent"
            onPress={() => router.replace('/sign-in')}
          >
            Back to sign in
          </Text>
        </View>
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
  linkWrap: { alignItems: 'center', paddingVertical: spacing.sm },
});
