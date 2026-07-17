import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner } from '@shared/ui/primitives/Banner';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { TextField } from '@shared/ui/primitives/TextField';
import { spacing } from '@shared/ui/theme';

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
  /** Toggle-link copy, split so the question reads white and the action cobalt. */
  linkQuestion: string;
  linkAction: string;
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
  linkQuestion,
  linkAction,
  showConfirm = false,
  enforcePasswordPolicy = false,
  showForgotPassword = false,
}: AuthFormProps): ReactElement {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');

  // Sign-up wants a brand-new credential; sign-in wants the saved one.
  const passwordContentType = showConfirm ? 'newPassword' : 'password';
  const passwordAutoComplete = showConfirm ? 'new-password' : 'password';

  const emailValid = isValidEmail(email);
  const passwordIssues = enforcePasswordPolicy ? validatePassword(password) : [];
  const confirmValid = showConfirm ? passwordsMatch(password, confirm) : true;

  const showEmailError = email.length > 0 && !emailValid;
  const showPasswordError = passwordIssues.length > 0 && password.length > 0;
  const showConfirmError = showConfirm && confirm.length > 0 && !confirmValid;

  const formValid =
    emailValid && password.length > 0 && passwordIssues.length === 0 && confirmValid;

  return (
    <AuthHeroLayout testID={screenTestID} tagline={tagline} background={false}>
      <View style={styles.form}>
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
        <TextField
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="Password"
          secure
          autoCapitalize="none"
          textContentType={passwordContentType}
          autoComplete={passwordAutoComplete}
          error={showPasswordError}
        />
        {showPasswordError ? (
          <Text testID="password-error" variant="caption" tone="danger">
            {PASSWORD_REQUIREMENTS_HINT}
          </Text>
        ) : null}
        {showConfirm ? (
          <TextField
            testID="confirm-input"
            value={confirm}
            onChangeText={setConfirm}
            placeholder="Confirm password"
            secure
            autoCapitalize="none"
            textContentType="newPassword"
            autoComplete="new-password"
            error={showConfirmError}
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
            <Text variant="label" tone="secondary">
              {linkQuestion}{' '}
              <Text variant="label" tone="accent">
                {linkAction}
              </Text>
            </Text>
          </Link>
        </View>
      </View>
    </AuthHeroLayout>
  );
}

const styles = StyleSheet.create({
  form: { gap: spacing.sm },
  forgotRow: { alignItems: 'flex-end', marginVertical: spacing.xs },
  linkWrap: { alignItems: 'center', paddingTop: spacing.sm },
});
