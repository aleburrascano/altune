import { Link } from 'expo-router';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import { useEmailPasswordFields } from '@features/auth/hooks/useEmailPasswordFields';
import { useSignIn } from '@features/auth/hooks/useSignIn';
import { AuthForm } from '@features/auth/ui/AuthForm';
import { EmailPasswordFields } from '@features/auth/ui/AuthFields';

const GENERIC_SIGN_IN_ERROR = "Couldn't sign you in. Please try again.";

export function SignInScreen(): ReactElement {
  const { state, signIn } = useSignIn();
  const fields = useEmailPasswordFields();

  const canSubmit = fields.emailValid && fields.password.length > 0;

  return (
    <AuthForm
      screenTestID="sign-in-screen"
      tagline="Welcome back."
      submitLabel="Sign in"
      onSubmit={() => void signIn(fields.credentials.email, fields.credentials.password)}
      pending={state.kind === 'pending'}
      canSubmit={canSubmit}
      state={state}
      generic={GENERIC_SIGN_IN_ERROR}
      linkHref="/sign-up"
      linkTestID="link-to-sign-up"
      linkQuestion="No account?"
      linkAction="Sign up"
    >
      <EmailPasswordFields
        email={fields.email}
        onChangeEmail={fields.setEmail}
        showEmailError={fields.showEmailError}
        password={fields.password}
        onChangePassword={fields.setPassword}
        passwordKind="existing"
        showPasswordError={false}
      />
      <View style={styles.forgotRow}>
        <Link href="/forgot-password" testID="link-to-forgot-password">
          <Text variant="caption" tone="accent">
            Forgot password?
          </Text>
        </Link>
      </View>
    </AuthForm>
  );
}

const styles = StyleSheet.create({
  forgotRow: { alignItems: 'flex-end', marginVertical: spacing.xs },
});
