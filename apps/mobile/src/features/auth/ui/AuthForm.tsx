import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import {
  PASSWORD_REQUIREMENTS_HINT,
  isValidEmail,
  passwordsMatch,
  validatePassword,
} from '../lib/validation';
import { OAuthButtons } from './OAuthButtons';

/**
 * AuthForm — the shared presentational shell for SignIn and SignUp.
 *
 * The two screens are identical except for their copy, link target, and
 * which auth hook they dispatch into; this component owns the form state +
 * layout, and the screens pass the per-mode bits as props. Extracted after
 * the 3 clone groups fallow flagged across the two screens.
 *
 * Validation (auth-hardening spec): email format is checked on both screens
 * (AC#2). Sign-up additionally enforces the password policy (AC#3) and a
 * confirm-password match (AC#1) via `enforcePasswordPolicy` + `showConfirm`.
 * Submit stays disabled until the active rules pass — malformed input never
 * costs a network round-trip. Server validation remains the backstop.
 */
type AuthFormProps = {
  screenTestID: string;
  title: string;
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
  title,
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

  // Errors surface only once the field has content — a pristine empty field
  // is "incomplete", not "wrong".
  const showEmailError = email.length > 0 && !emailValid;
  const showPasswordError = passwordIssues.length > 0 && password.length > 0;
  const showConfirmError = showConfirm && confirm.length > 0 && !confirmValid;

  const formValid =
    emailValid && password.length > 0 && passwordIssues.length === 0 && confirmValid;

  return (
    <Screen testID={screenTestID}>
      <View style={styles.body}>
        <View style={styles.header}>
          <Wordmark size={40} />
          <Text variant="title">{title}</Text>
        </View>
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
          <Text testID="email-error" variant="caption" tone="danger" style={styles.fieldError}>
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
          <Text testID="password-error" variant="caption" tone="danger" style={styles.fieldError}>
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
          <Text testID="confirm-error" variant="caption" tone="danger" style={styles.fieldError}>
            Passwords don&apos;t match.
          </Text>
        ) : null}
        <Button
          testID="submit-button"
          label={submitLabel}
          onPress={() => onSubmit(email.trim(), password)}
          loading={pending}
          disabled={!formValid}
        />
        {showForgotPassword ? (
          <View style={styles.linkWrap}>
            <Link href="/forgot-password" testID="link-to-forgot-password">
              <Text variant="label" tone="accent">
                Forgot password?
              </Text>
            </Link>
          </View>
        ) : null}
        <View style={styles.linkWrap}>
          <Link href={linkHref} testID={linkTestID}>
            <Text variant="label" tone="accent">
              {linkText}
            </Text>
          </Link>
        </View>
        {hasError ? (
          <Banner testID="auth-error" tone="danger" style={styles.errorBanner}>
            {errorText}
          </Banner>
        ) : null}
        <OAuthButtons />
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
  fieldError: { marginTop: -spacing.xs },
  linkWrap: { alignItems: 'center', paddingVertical: spacing.sm },
  errorBanner: { marginTop: spacing.sm },
});
