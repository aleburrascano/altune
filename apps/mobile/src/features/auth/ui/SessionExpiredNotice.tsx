import { useSignOut } from '@shared/auth/useSignOut';
import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';

import { AuthFullScreenNotice } from './AuthFullScreenNotice';

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
        testID="session-expired-signin"
        label={state.status === 'loading' ? 'Signing out…' : 'Sign in again'}
        disabled={state.status === 'loading'}
        onPress={() => {
          void signOut();
        }}
      />
    </AuthFullScreenNotice>
  );
}
