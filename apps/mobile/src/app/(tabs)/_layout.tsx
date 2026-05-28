import { Tabs } from 'expo-router';

import { GlassTabBar } from '../../shared/ui/navigation/GlassTabBar';

// AIDEV-NOTE: ADR-0008 — tabbed app shell (Discover + Library) with the custom
// floating GlassTabBar. Lives in the (tabs) route group so the URLs stay clean
// (/discover, /library) and AuthGate's redirects (/library) keep working
// unchanged. Add a tab by dropping a file here + a <Tabs.Screen>.
export default function TabsLayout() {
  return (
    <Tabs screenOptions={{ headerShown: false }} tabBar={(props) => <GlassTabBar {...props} />}>
      <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
      <Tabs.Screen name="library" options={{ title: 'Library' }} />
    </Tabs>
  );
}
