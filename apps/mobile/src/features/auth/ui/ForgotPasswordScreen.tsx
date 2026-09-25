import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { TextField } from '@shared/ui/primitives/TextField';
import { spacing } from '@shared/ui/theme';

import { useResetPassword } from '../hooks/useResetPassword';
import { isValidEmail } from '../validation';
import { AuthErrorBanner } from './AuthErrorBanner';
import { BackToSignInLink } from './BackToSignInLink';
import { FieldError } from './FieldError';
import { AuthHeroLayout } from './hero/AuthHeroLayout';

const GENERIC_ERROR = "Couldn't send the reset email. Please try again.";
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
          <BackToSignInLink />
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
            <FieldError testID="email-error">Enter a valid email address.</FieldError>
          ) : null}
          <Button
            testID="submit-button"
            label="Send reset link"
            onPress={() => void requestReset(email)}
            loading={state.kind === 'pending'}
            disabled={!emailValid}
          />
          <AuthErrorBanner state={state} generic={GENERIC_ERROR} />
          <BackToSignInLink />
        </View>
      )}
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.md },
});
