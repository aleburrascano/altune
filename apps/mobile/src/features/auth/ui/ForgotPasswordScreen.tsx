import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { TextField } from '@shared/ui/primitives/TextField';
import { spacing } from '@shared/ui/theme';

import { useResetPassword } from '../hooks/useResetPassword';
import { authErrorText } from '../lib/errorCopy';
import { isValidEmail } from '../lib/validation';
import { AuthHeroLayout } from './hero/AuthHeroLayout';

const GENERIC_ERROR = "Couldn't send the reset email. Please try again.";
// Anti-enumeration: identical message whether or not the address has an account.
const SENT_COPY =
  "If an account exists for that email, we've sent a reset link. Check your email and follow it to choose a new password.";

export function ForgotPasswordScreen(): ReactElement {
  const { state, requestReset } = useResetPassword();
  const [email, setEmail] = useState('');

  const emailValid = isValidEmail(email);
  const showEmailError = email.length > 0 && !emailValid;

  return (
    <AuthHeroLayout testID="forgot-password-screen" background={false}>
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
          <TextField
            testID="email-input"
            value={email}
            onChangeText={setEmail}
            placeholder="Email"
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="email-address"
            textContentType="emailAddress"
            autoComplete="email"
            error={showEmailError}
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
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
