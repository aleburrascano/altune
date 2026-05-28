import { Link } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Screen } from '@shared/ui/primitives/Screen';
import { Text } from '@shared/ui/primitives/Text';
import { Wordmark } from '@shared/ui/primitives/Wordmark';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import { useSignUp } from '../hooks/useSignUp';

export function SignUpScreen() {
  const theme = useTheme();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const { state, signUp } = useSignUp();

  const fieldColors = {
    borderColor: theme.color.border,
    backgroundColor: theme.color.surface1,
    color: theme.color.textPrimary,
  };

  return (
    <Screen testID="sign-up-screen">
      <View style={styles.body}>
        <View style={styles.header}>
          <Wordmark size={40} />
          <Text variant="title">Create your account</Text>
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
          label="Sign up"
          onPress={() => void signUp(email, password)}
          loading={state.kind === 'pending'}
        />
        <View style={styles.linkWrap}>
          <Link href="/sign-in" testID="link-to-sign-in">
            <Text variant="label" tone="accent">
              Have an account? Sign in
            </Text>
          </Link>
        </View>
        {state.kind === 'error' ? (
          <Text testID="auth-error" variant="caption" tone="danger" style={styles.error}>
            Sign up failed. Check your details and try again.
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
