import { useState } from 'react';
import { Tabs } from 'expo-router';
import { View } from 'react-native';

import { useActiveDownloads } from '../../shared/acquisition/useActiveDownloads';
import { DownloadsBar } from '../../shared/acquisition/ui/DownloadsBar';
import { DownloadsSheet } from '../../shared/acquisition/ui/DownloadsSheet';
import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { TabBar } from '../../shared/ui/navigation/TabBar';

// AIDEV-NOTE: the unified bottom dock — the in-flight downloads bar stacked over
// the now-playing MiniPlayer, each absent when inactive. Composed here at the
// shell because it bridges the acquisition subsystem and the playback feature,
// and neither may import the other; the tab layout is the composition root that
// legitimately depends on both. (Was features/playback/ui/ActivityDock; the pure
// download views now live with their data under shared/acquisition/ui.)
function ActivityDock() {
  const downloads = useActiveDownloads();
  const [sheetOpen, setSheetOpen] = useState(false);

  return (
    <View>
      {downloads.length > 0 ? (
        <DownloadsBar items={downloads} onPress={() => setSheetOpen(true)} />
      ) : null}
      <MiniPlayer />
      <DownloadsSheet
        visible={sheetOpen && downloads.length > 0}
        items={downloads}
        onClose={() => setSheetOpen(false)}
      />
    </View>
  );
}

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
          <ActivityDock />
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
