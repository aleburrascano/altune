import { Tabs } from 'expo-router';
import { View } from 'react-native';

import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { TabBar } from '../../shared/ui/navigation/TabBar';

// AIDEV-NOTE: tabbed app shell (Discover + Library) with the custom docked
// TabBar. Each tab is a directory with its own Stack _layout for nested
// navigation (discover/detail, library/detail, etc.). Add a tab by
// creating a directory here with _layout.tsx + index.tsx + a <Tabs.Screen>.
// MiniPlayer sits above the TabBar when a track is loaded (audio-playback-v1).
export default function TabsLayout() {
  return (
    <View style={{ flex: 1 }}>
      <Tabs screenOptions={{ headerShown: false }} tabBar={(props) => (
        <>
          <MiniPlayer />
          <TabBar {...props} />
        </>
      )}>
        <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
        <Tabs.Screen name="library" options={{ title: 'Library' }} />
        <Tabs.Screen name="settings" options={{ title: 'Settings' }} />
      </Tabs>
    </View>
  );
}
