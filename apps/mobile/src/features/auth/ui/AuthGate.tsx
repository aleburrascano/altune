import { Redirect } from 'expo-router';
import { Text, View } from 'react-native';

import { useSession } from '../hooks/useSession';

// Splash while we don't know yet; redirect to /sign-in while signed out;
// render children once we have a session. Per ADR-0006 / spec AC#6.
export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();

  if (session.status === 'loading') {
    return (
      <View
        testID="auth-splash"
        style={{
          flex: 1,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: '#fff',
        }}
      >
        <Text style={{ color: '#111' }}>Loading…</Text>
      </View>
    );
  }

  if (session.status === 'signed-out') {
    return <Redirect href="/sign-in" />;
  }

  return <>{children}</>;
}
