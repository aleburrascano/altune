import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { AppLink } from '@shared/navigation';
import { Text, spacing } from '@shared/ui';
import { useEmailPasswordFields } from '@features/signIn/view/useEmailPasswordFields';
import { useSignIn } from '@features/signIn/view/useSignIn';
import { AuthForm } from '@features/signIn/view/AuthForm';
import { EmailPasswordFields } from '@features/signIn/view/AuthFields';

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
        <AppLink href="/forgot-password" testID="link-to-forgot-password">
          <Text variant="caption" tone="accent">
            Forgot password?
          </Text>
        </AppLink>
      </View>
    </AuthForm>
  );
}

const styles = StyleSheet.create({
  forgotRow: { alignItems: 'flex-end', marginVertical: spacing.xs },
});
