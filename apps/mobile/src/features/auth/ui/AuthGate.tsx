import { Redirect, useSegments } from 'expo-router';
import { Text, View } from 'react-native';

import { useSession } from '../hooks/useSession';

// Splash while we don't know yet; redirect into / out of the (auth) group
// based on session state. Per ADR-0006 / spec AC#6.
//
// AIDEV-NOTE: We check `useSegments()[0] === '(auth)'` so we don't redirect
// signed-out users to /sign-in WHEN THEY'RE ALREADY ON /sign-in — without
// this guard, AuthGate wraps the (auth) route too, re-evaluates after the
// Redirect mounts, sees signed-out again, redirects again, ad infinitum.
export function AuthGate({ children }: { children: React.ReactNode }) {
  const session = useSession();
  const segments = useSegments();
  const inAuthGroup = segments[0] === '(auth)';

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

  if (session.status === 'signed-out' && !inAuthGroup) {
    return <Redirect href="/sign-in" />;
  }

  if (session.status === 'signed-in' && inAuthGroup) {
    return <Redirect href="/library" />;
  }

  return <>{children}</>;
}
