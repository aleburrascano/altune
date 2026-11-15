import type { ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';

import { useSignOut } from '@shared/auth/useSignOut';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

type AuthFullScreenNoticeProps = {
  children: ReactNode;
  padded?: boolean;
  testID?: string;
};

export function AuthFullScreenNotice({
  children,
  padded = false,
  testID,
}: AuthFullScreenNoticeProps) {
  const theme = useTheme();

  return (
    <View
      testID={testID}
      style={[
        styles.notice,
        padded ? styles.padded : null,
        { backgroundColor: theme.color.canvas },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  notice: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md },
  padded: { padding: spacing.lg },
});

export function SessionExpiredNotice() {
  const { state, signOut } = useSignOut();

  return (
    <AuthFullScreenNotice testID="session-expired" padded>
      <Text variant="displayL" style={{ textAlign: 'center' }}>
        Your session expired
      </Text>
      <Text variant="body" tone="secondary" style={{ textAlign: 'center' }}>
        Sign in again to get back to your library. Nothing has been lost.
      </Text>
      <Button
        action="auth.session_expired.sign_in_again"
        testID="session-expired-signin"
        label={state.status === 'loading' ? 'Signing out…' : 'Sign in again'}
        disabled={state.status === 'loading'}
        onPress={() => {
          void signOut();
        }}
      />
      {state.status === 'error' ? (
        <Text
          testID="session-expired-error"
          variant="body"
          tone="danger"
          style={{ textAlign: 'center' }}
        >
          Couldn't reach the server. You've been signed out on this device.
        </Text>
      ) : null}
    </AuthFullScreenNotice>
  );
}

export function InvalidRecoveryLinkNotice() {
  const router = useRouter();

  return (
    <AuthFullScreenNotice testID="invalid-recovery-link" padded>
      <Text variant="displayL" style={{ textAlign: 'center' }}>
        This link isn&apos;t valid
      </Text>
      <Text variant="body" tone="secondary" style={{ textAlign: 'center' }}>
        Password reset links expire and can only be opened once. Request a new one from the sign-in
        screen to choose a new password.
      </Text>
      <Button
        action="auth.invalid_recovery_link.back_to_sign_in"
        testID="invalid-recovery-link-signin"
        label="Back to sign in"
        onPress={() => {
          router.replace('/sign-in');
        }}
      />
    </AuthFullScreenNotice>
  );
}
