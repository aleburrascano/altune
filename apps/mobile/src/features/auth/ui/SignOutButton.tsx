import { Button } from '@shared/ui/primitives/Button';

import { useSignOut } from '../hooks/useSignOut';

export function SignOutButton() {
  const { state, signOut } = useSignOut();
  return (
    <Button
      testID="sign-out-button"
      variant="ghost"
      label={state.kind === 'pending' ? 'Signing out…' : 'Sign out'}
      onPress={() => void signOut()}
      loading={state.kind === 'pending'}
    />
  );
}
