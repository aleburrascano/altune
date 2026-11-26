import { NavigationBar } from 'expo-navigation-bar';
import type { ReactElement } from 'react';

import { navigationBarStyleFor } from '@/mobile/frame/navigationBarStyle';

export function SystemNavigationBar({ scheme }: { scheme: 'light' | 'dark' }): ReactElement | null {
  return <NavigationBar style={navigationBarStyleFor(scheme)} />;
}

export function PlaybackShortcuts(): null {
  return null;
}

export function TabsTitle(_props: { pathname: string }): ReactElement | null {
  return null;
}
