import { Tabs } from 'expo-router';

import { TabBar } from '../../shared/ui/navigation/TabBar';

// AIDEV-NOTE: tabbed app shell (Discover + Library) with the custom docked
// TabBar. Lives in the (tabs) route group so the URLs stay clean
// (/discover, /library) and AuthGate's redirects (/library) keep working
// unchanged. Add a tab by dropping a file here + a <Tabs.Screen>.
export default function TabsLayout() {
  return (
    <Tabs screenOptions={{ headerShown: false }} tabBar={(props) => <TabBar {...props} />}>
      <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
      <Tabs.Screen name="library" options={{ title: 'Library' }} />
      <Tabs.Screen name="detail" options={{ href: null }} />
    </Tabs>
  );
}
