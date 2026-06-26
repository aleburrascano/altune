import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useResetPassword } from '../hooks/useResetPassword';
import { authErrorText } from '../lib/errorCopy';
import { isValidEmail } from '../lib/validation';
import { AuthHeroLayout } from './hero/AuthHeroLayout';

const GENERIC_ERROR = "Couldn't send the reset email. Please try again.";
// Anti-enumeration: identical message whether or not the address has an account.
const SENT_COPY =
  "If an account exists for that email, we've sent a reset link. Check your email and follow it to choose a new password.";

export function ForgotPasswordScreen(): ReactElement {
  const theme = useTheme();
  const { state, requestReset } = useResetPassword();
  const [email, setEmail] = useState('');

  const emailValid = isValidEmail(email);
  const showEmailError = email.length > 0 && !emailValid;

  return (
    <AuthHeroLayout testID="forgot-password-screen">
      {state.kind === 'sent' ? (
        <View style={styles.form}>
          <Text variant="title">Check your email</Text>
          <Banner testID="reset-sent" tone="info">
            {SENT_COPY}
          </Banner>
          <View style={styles.linkWrap}>
            <Link href="/sign-in" testID="back-to-sign-in">
              <Text variant="label" tone="accent">
                Back to sign in
              </Text>
            </Link>
          </View>
        </View>
      ) : (
        <View style={styles.form}>
          <Text variant="title">Reset your password</Text>
          <Text variant="label" tone="secondary">
            Enter your email and we&apos;ll send you a link to choose a new password.
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
          <View style={styles.linkWrap}>
            <Link href="/sign-in" testID="back-to-sign-in">
              <Text variant="label" tone="accent">
                Back to sign in
              </Text>
            </Link>
          </View>
        </View>
      )}
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.md },
  input: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
