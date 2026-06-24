import { Link } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { radius, spacing, useTheme } from '@shared/ui/theme';

/**
 * AuthForm — the shared presentational shell for SignIn and SignUp.
 *
 * The two screens are identical except for their copy, link target, and
 * which auth hook they dispatch into; this component owns the email/password
 * state + layout, and the screens pass the per-mode bits as props. Extracted
 * after the 3 clone groups fallow flagged across the two screens.
 */
export type AuthFormProps = {
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
}: AuthFormProps): ReactElement {
  const theme = useTheme();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');

  const fieldColors = {
    borderColor: theme.color.border,
    backgroundColor: theme.color.surface1,
    color: theme.color.textPrimary,
  };

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
        <TextInput
          testID="password-input"
          value={password}
          onChangeText={setPassword}
          placeholder="Password"
          placeholderTextColor={theme.color.textTertiary}
          secureTextEntry
          style={[styles.input, fieldColors]}
        />
        <Button
          testID="submit-button"
          label={submitLabel}
          onPress={() => onSubmit(email, password)}
          loading={pending}
        />
        <View style={styles.linkWrap}>
          <Link href={linkHref} testID={linkTestID}>
            <Text variant="label" tone="accent">
              {linkText}
            </Text>
          </Link>
        </View>
        {hasError ? (
          <Text testID="auth-error" variant="caption" tone="danger" style={styles.error}>
            {errorText}
          </Text>
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
  linkWrap: { alignItems: 'center', paddingVertical: spacing.sm },
  error: { textAlign: 'center' },
});
