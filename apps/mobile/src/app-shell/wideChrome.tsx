import type { ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { SidebarPlaylists } from '@features/library/ui/SidebarPlaylists';
import { PlayerBar } from '@features/playback/ui/PlayerBar';
import { Sidebar } from '@shared/ui/navigation/Sidebar';
import type { TabRoute } from '@shared/ui/navigation/tabRoutes';

export type WideChromeProps = {
  activeRoute: string;
  onNavigate: (route: TabRoute) => void;
  children: ReactNode;
};

function WideChromeRow({ activeRoute, onNavigate, children }: WideChromeProps) {
  return (
    <View style={styles.row}>
      <Sidebar activeRoute={activeRoute} onNavigate={onNavigate} playlists={<SidebarPlaylists />} />
      <View style={styles.content}>{children}</View>
    </View>
  );
}

export function WideChrome(props: WideChromeProps) {
  return (
    <View style={styles.root}>
      <WideChromeRow {...props} />
      <PlayerBar />
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, flexDirection: 'column' },
  row: { flex: 1, flexDirection: 'row' },
  content: { flex: 1 },
});
