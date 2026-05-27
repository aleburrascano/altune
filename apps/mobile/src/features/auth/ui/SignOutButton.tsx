import { Pressable, Text } from 'react-native';

import { useSignOut } from '../hooks/useSignOut';

export function SignOutButton() {
  const { state, signOut } = useSignOut();
  return (
    <Pressable
      testID="sign-out-button"
      onPress={() => void signOut()}
      disabled={state.kind === 'pending'}
      style={{ padding: 8 }}
    >
      <Text>{state.kind === 'pending' ? 'Signing out…' : 'Sign out'}</Text>
    </Pressable>
  );
}
