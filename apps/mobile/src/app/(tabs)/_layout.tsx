import { usePathname, useRouter } from 'expo-router';
import type { BottomTabBarProps } from 'expo-router/js-tabs';
import { Tabs } from 'expo-router/js-tabs';
import Head from 'expo-router/head';
import { useState } from 'react';
import { Platform, View } from 'react-native';

import { useActiveDownloadItems } from '../../shared/acquisition/downloadStore';
import { DownloadsBar } from '../../shared/acquisition/ui/DownloadsBar';
import { DownloadsSheet } from '../../shared/acquisition/ui/DownloadsSheet';
import { MiniPlayer } from '../../features/playback/ui/MiniPlayer';
import { SidebarPlaylists } from '../../features/library/ui/SidebarPlaylists';
import { usePlaylistActions } from '../../features/library/hooks/usePlaylistActions';
import { useWideWebLayout } from '../../shared/ui/layout/useWideWebLayout';
import { pageTitleFor } from '../../shared/ui/navigation/pageTitle';
import { Sidebar } from '../../shared/ui/navigation/Sidebar';
import { TabBar } from '../../shared/ui/navigation/TabBar';
import { TAB_ROUTES, type TabRoute } from '../../shared/ui/navigation/tabRoutes';

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
  const pathname = usePathname();
  const activeRoute = activeTabRouteFor(pathname);
  const router = useRouter();
  return { activeRoute, pathname, onNavigate: (route: TabRoute) => router.push(`/${route}`) };
}

const PLAYLIST_PATH_PREFIX = '/library/playlist/';

function usePlaylistNameFromPath(pathname: string): string | undefined {
  const { playlists } = usePlaylistActions();
  const [, playlistId] = pathname.match(/^\/library\/playlist\/([^/]+)/) ?? [];
  return playlistId ? playlists.find((playlist) => playlist.id === playlistId)?.name : undefined;
}

function PlaylistTabsTitle({ pathname }: { pathname: string }) {
  const playlistName = usePlaylistNameFromPath(pathname);
  return (
    <Head>
      <title>{pageTitleFor(pathname, playlistName)}</title>
    </Head>
  );
}

function TabsTitle({ pathname }: { pathname: string }) {
  if (Platform.OS !== 'web') return null;
  if (pathname.startsWith(PLAYLIST_PATH_PREFIX)) return <PlaylistTabsTitle pathname={pathname} />;
  return (
    <Head>
      <title>{pageTitleFor(pathname)}</title>
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
  const isWide = useWideWebLayout();
  const { activeRoute, pathname, onNavigate } = useTabsNavigation();
  return (
    <View style={tabsLayoutStyle(isWide)}>
      <TabsTitle pathname={pathname} />
      <TabsSidebar show={isWide} activeRoute={activeRoute} onNavigate={onNavigate} />
      <TabsContent isWide={isWide} />
    </View>
  );
}
