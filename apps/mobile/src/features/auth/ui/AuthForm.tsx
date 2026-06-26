import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import {
  PASSWORD_REQUIREMENTS_HINT,
  isValidEmail,
  passwordsMatch,
  validatePassword,
} from '../lib/validation';
import { AuthHeroLayout } from './hero/AuthHeroLayout';
import { OAuthButtons } from './OAuthButtons';

/**
 * Shared sign-in / sign-up form, rendered in the AuthHeroLayout (artwork hero
 * on top, this form bottom-anchored). The screens pass per-mode copy + which
 * extras to show. Validation (email format always; password policy + confirm
 * on sign-up) gates submit before any network call; server stays the backstop.
 */
type AuthFormProps = {
  screenTestID: string;
  tagline: string;
  submitLabel: string;
  onSubmit: (email: string, password: string) => void;
  pending: boolean;
  hasError: boolean;
  errorText: string;
  linkHref: '/sign-in' | '/sign-up';
  linkTestID: string;
  linkText: string;
  /** Render a confirm-password field and require it to match (sign-up). */
  showConfirm?: boolean;
  /** Enforce the password policy before enabling submit (sign-up). */
  enforcePasswordPolicy?: boolean;
  /** Render a "Forgot password?" link to the reset flow (sign-in). */
  showForgotPassword?: boolean;
};

export function AuthForm({
  screenTestID,
  tagline,
  submitLabel,
  onSubmit,
  pending,
  hasError,
  errorText,
  linkHref,
  linkTestID,
  linkText,
  showConfirm = false,
  enforcePasswordPolicy = false,
  showForgotPassword = false,
}: AuthFormProps): ReactElement {
  const theme = useTheme();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');

  const fieldColors = {
    borderColor: theme.color.border,
    backgroundColor: theme.color.surface1,
    color: theme.color.textPrimary,
  };

  const emailValid = isValidEmail(email);
  const passwordIssues = enforcePasswordPolicy ? validatePassword(password) : [];
  const confirmValid = showConfirm ? passwordsMatch(password, confirm) : true;

  const showEmailError = email.length > 0 && !emailValid;
  const showPasswordError = passwordIssues.length > 0 && password.length > 0;
  const showConfirmError = showConfirm && confirm.length > 0 && !confirmValid;

  const formValid =
    emailValid && password.length > 0 && passwordIssues.length === 0 && confirmValid;

  return (
    <AuthHeroLayout testID={screenTestID} tagline={tagline}>
      <View style={styles.form}>
        <TextInput
          testID="email-input"
          value={email}
          onChangeText={setEmail}
          placeholder="Email"
          placeholderTextColor={theme.color.textTertiary}
          autoCapitalize="none"
          keyboardType="email-address"
          style={[styles.input, fieldColors]}
        />
        {showEmailError ? (
          <Text testID="email-error" variant="caption" tone="danger">
            Enter a valid email address.
          </Text>
        ) : null}
        <TextInput
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="Password"
          placeholderTextColor={theme.color.textTertiary}
          secureTextEntry
          style={[styles.input, fieldColors]}
        />
        {showPasswordError ? (
          <Text testID="password-error" variant="caption" tone="danger">
            {PASSWORD_REQUIREMENTS_HINT}
          </Text>
        ) : null}
        {showConfirm ? (
          <TextInput
            testID="confirm-input"
            value={confirm}
            onChangeText={setConfirm}
            placeholder="Confirm password"
            placeholderTextColor={theme.color.textTertiary}
            secureTextEntry
            style={[styles.input, fieldColors]}
          />
        ) : null}
        {showConfirmError ? (
          <Text testID="confirm-error" variant="caption" tone="danger">
            Passwords don&apos;t match.
          </Text>
        ) : null}
        {showForgotPassword ? (
          <View style={styles.forgotRow}>
            <Link href="/forgot-password" testID="link-to-forgot-password">
              <Text variant="caption" tone="accent">
                Forgot password?
              </Text>
            </Link>
          </View>
        ) : null}
        {hasError ? (
          <Banner testID="auth-error" tone="danger">
            {errorText}
          </Banner>
        ) : null}
        <Button
          testID="submit-button"
          label={submitLabel}
          onPress={() => onSubmit(email.trim(), password)}
          loading={pending}
          disabled={!formValid}
        />
        <OAuthButtons />
        <View style={styles.linkWrap}>
          <Link href={linkHref} testID={linkTestID}>
            <Text variant="label" tone="accent">
              {linkText}
            </Text>
          </Link>
        </View>
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.sm },
  input: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
  forgotRow: { alignItems: 'flex-end', marginTop: -spacing.xs },
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
