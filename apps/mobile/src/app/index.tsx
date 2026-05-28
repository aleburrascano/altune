import { Redirect } from 'expo-router';

// AIDEV-NOTE: Until there's a real home screen (search, recent plays, etc.),
// `/` lands on the library — the only user-visible feature in v1.
// Replace with a real home component when more features land.

export default function HomeScreen() {
  return <Redirect href="/discover" />;
}
