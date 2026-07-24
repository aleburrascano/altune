import { View } from 'react-native';

import { useSignOut } from '@shared/auth/useSignOut';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

export function SessionExpiredNotice() {
  const theme = useTheme();
  const { state, signOut } = useSignOut();

  return (
    <View
      testID="session-expired"
      style={{
        flex: 1,
        alignItems: 'center',
        justifyContent: 'center',
        gap: spacing.md,
        padding: spacing.lg,
        backgroundColor: theme.color.canvas,
      }}
    >
      <Text variant="displayL" style={{ textAlign: 'center' }}>
        Your session expired
      </Text>
      <Text variant="body" tone="secondary" style={{ textAlign: 'center' }}>
        Sign in again to get back to your library. Nothing has been lost.
      </Text>
      <Button
        testID="session-expired-signin"
        label={state.kind === 'pending' ? 'Signing out…' : 'Sign in again'}
        disabled={state.kind === 'pending'}
        onPress={() => {
          void signOut();
        }}
      />
    </View>
  );
}
