import { Stack } from 'expo-router';

// AIDEV-NOTE: The (auth) route group lives OUTSIDE the AuthGate's redirect
// scope so signed-out users can reach /sign-in and /sign-up. Slice 10 stubs
// the screens; real forms land in Slices 11/12.

export default function AuthLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
