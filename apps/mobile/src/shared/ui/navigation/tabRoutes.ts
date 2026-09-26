import { Compass, Library as LibraryIcon, Settings } from 'lucide-react-native';
import type { ComponentType } from 'react';

export type TabRoute = 'discover' | 'library' | 'settings';

export type TabIcon = ComponentType<{ size?: number; color?: string }>;

export type TabRouteInfo = {
  route: TabRoute;
  label: string;
  Icon: TabIcon;
};

export const TAB_ROUTE_INFO_BY_ROUTE: Record<TabRoute, TabRouteInfo> = {
  discover: { route: 'discover', label: 'Discover', Icon: Compass },
  library: { route: 'library', label: 'Library', Icon: LibraryIcon },
  settings: { route: 'settings', label: 'Settings', Icon: Settings },
};

export const TAB_ROUTES: readonly TabRoute[] = ['discover', 'library', 'settings'];
