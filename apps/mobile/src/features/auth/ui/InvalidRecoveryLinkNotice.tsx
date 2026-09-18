import { useRouter } from 'expo-router';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';

import { AuthFullScreenNotice } from './AuthFullScreenNotice';

// Shown when the reset-password route is reached without a verified recovery
// exchange — a bare `altune://reset-password` deep link, or an expired unlock
// window. It never renders the password form, so it cannot change a password.
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
        testID="invalid-recovery-link-signin"
        label="Back to sign in"
        onPress={() => {
          router.replace('/sign-in');
        }}
      />
    </AuthFullScreenNotice>
  );
}
