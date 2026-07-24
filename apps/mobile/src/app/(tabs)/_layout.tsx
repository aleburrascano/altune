import { useState } from 'react';
import { Tabs } from 'expo-router';
import { View } from 'react-native';

import { useActiveDownloads } from '../../shared/acquisition/useActiveDownloads';
import { DownloadsBar } from '../../shared/acquisition/ui/DownloadsBar';
import { DownloadsSheet } from '../../shared/acquisition/ui/DownloadsSheet';
import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { TabBar } from '../../shared/ui/navigation/TabBar';

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

export default function TabsLayout() {
  return (
    <View style={{ flex: 1 }}>
      <Tabs
        screenOptions={{ headerShown: false }}
        tabBar={(props) => (
          <>
            <ActivityDock />
            <TabBar {...props} />
          </>
        )}
      >
        <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
        <Tabs.Screen name="library" options={{ title: 'Library' }} />
        <Tabs.Screen name="settings" options={{ title: 'Settings' }} />
      </Tabs>
    </View>
  );
}
