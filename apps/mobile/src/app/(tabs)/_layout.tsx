import type { BottomTabBarProps } from 'expo-router/js-tabs';
import { Tabs } from 'expo-router/js-tabs';
import { usePathname } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useActiveDownloadItems } from '../../shared/acquisition/downloadStore';
import { DownloadsBar } from '../../shared/acquisition/ui/DownloadsBar';
import { DownloadsSheet } from '../../shared/acquisition/ui/DownloadsSheet';
import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { useWideWebLayout } from '../../shared/ui/layout/useWideWebLayout';
import { TabBar } from '../../shared/ui/navigation/TabBar';
import { TabsTitle } from '../../app-shell/platformExtras';

function ActivityDock() {
  const downloads = useActiveDownloadItems();
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

function tabsTabBar(isWide: boolean) {
  return function TabsBar(props: BottomTabBarProps) {
    return (
      <>
        <ActivityDock />
        {isWide ? null : <TabBar {...props} />}
      </>
    );
  };
}

function TabsContent({ isWide }: { isWide: boolean }) {
  return (
    <Tabs screenOptions={{ headerShown: false }} tabBar={tabsTabBar(isWide)}>
      <Tabs.Screen name="discover" options={{ title: 'Discover' }} />
      <Tabs.Screen name="library" options={{ title: 'Library' }} />
      <Tabs.Screen name="settings" options={{ title: 'Settings' }} />
    </Tabs>
  );
}

export default function TabsLayout() {
  const isWide = useWideWebLayout();
  const pathname = usePathname();

  return (
    <View style={styles.container}>
      <TabsTitle pathname={pathname} />
      <TabsContent isWide={isWide} />
    </View>
  );
}

const styles = { container: { flex: 1, flexDirection: 'column' as const } };
