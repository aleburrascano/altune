import { Redirect } from 'expo-router';

// AIDEV-NOTE: `/` redirects into the (tabs) shell, landing on /discover (the
// default surface in v1). Route groups are path-transparent, so "/discover"
// resolves to app/(tabs)/discover.tsx. Replace with a real home component if
// one is ever needed.

export default function HomeScreen() {
  return <Redirect href="/discover" />;
}
