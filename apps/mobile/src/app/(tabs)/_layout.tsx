import { usePathname, useRouter } from 'expo-router';
import type { BottomTabBarProps } from 'expo-router/js-tabs';
import { Tabs } from 'expo-router/js-tabs';
import Head from 'expo-router/head';
import { useState } from 'react';
import { View } from 'react-native';

import { useActiveDownloadItems } from '../../shared/acquisition/downloadStore';
import { DownloadsBar } from '../../shared/acquisition/ui/DownloadsBar';
import { DownloadsSheet } from '../../shared/acquisition/ui/DownloadsSheet';
import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { SidebarPlaylists } from '../../features/library/ui/SidebarPlaylists';
import { useLayoutMode } from '../../shared/ui/layout/useLayoutMode';
import { Sidebar } from '../../shared/ui/navigation/Sidebar';
import { TabBar } from '../../shared/ui/navigation/TabBar';
import { TAB_ROUTE_INFO_BY_ROUTE, TAB_ROUTES, type TabRoute } from '../../shared/ui/navigation/tabRoutes';

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

function activeTabRouteFor(pathname: string): TabRoute {
  const match = TAB_ROUTES.find((route) => pathname.startsWith(`/${route}`));
  return match ?? 'discover';
}

function useTabsNavigation() {
  const activeRoute = activeTabRouteFor(usePathname());
  const router = useRouter();
  return { activeRoute, onNavigate: (route: TabRoute) => router.push(`/${route}`) };
}

function TabsTitle({ activeRoute }: { activeRoute: TabRoute }) {
  const label = TAB_ROUTE_INFO_BY_ROUTE[activeRoute].label;
  return (
    <Head>
      <title>{`${label} · Altune`}</title>
    </Head>
  );
}

type TabsSidebarProps = { show: boolean; activeRoute: TabRoute; onNavigate: (route: TabRoute) => void };

function TabsSidebar({ show, activeRoute, onNavigate }: TabsSidebarProps) {
  if (!show) return null;
  return <Sidebar activeRoute={activeRoute} onNavigate={onNavigate} playlists={<SidebarPlaylists />} />;
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

function tabsLayoutStyle(isWide: boolean) {
  return { flex: 1, flexDirection: isWide ? ('row' as const) : ('column' as const) };
}

export default function TabsLayout() {
  const isWide = useLayoutMode() === 'wide';
  const { activeRoute, onNavigate } = useTabsNavigation();
  return (
    <View style={tabsLayoutStyle(isWide)}>
      <TabsTitle activeRoute={activeRoute} />
      <TabsSidebar show={isWide} activeRoute={activeRoute} onNavigate={onNavigate} />
      <TabsContent isWide={isWide} />
    </View>
  );
}
