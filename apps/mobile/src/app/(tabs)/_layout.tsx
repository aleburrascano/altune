import { Tabs } from 'expo-router';

import { TabBar } from '../../shared/ui/navigation/TabBar';

// AIDEV-NOTE: tabbed app shell (Discover + Library) with the custom docked
// TabBar. Each tab is a directory with its own Stack _layout for nested
// navigation (discover/detail, library/detail, etc.). Add a tab by
// creating a directory here with _layout.tsx + index.tsx + a <Tabs.Screen>.
export default function TabsLayout() {
  return (
    <Tabs screenOptions={{ headerShown: false }} tabBar={(props) => <TabBar {...props} />}>
      <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
      <Tabs.Screen name="library" options={{ title: 'Library' }} />
    </Tabs>
  );
}
