import { NavigationBar } from 'expo-navigation-bar';
import type { ReactElement } from 'react';

export function SystemNavigationBar({ scheme }: { scheme: 'light' | 'dark' }): ReactElement | null {
  return <NavigationBar style={scheme === 'dark' ? 'light' : 'dark'} />;
}

export function PlaybackShortcuts(): null {
  return null;
}

export function TabsTitle(_props: { pathname: string }): ReactElement | null {
  return null;
}
