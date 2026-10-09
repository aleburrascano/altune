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

const APP_NAME = 'Altune';
const GENERIC_PLAYLIST_TITLE = 'Playlist';
const PLAYLIST_PATH_PREFIX = '/library/playlist/';

function titleFor(label: string): string {
  return `${label} · ${APP_NAME}`;
}

function matchesRoute(pathname: string, candidate: string): boolean {
  return pathname === `/${candidate}` || pathname.startsWith(`/${candidate}/`);
}

export function pageTitleFor(pathname: string, playlistName?: string | null): string {
  if (pathname.startsWith(PLAYLIST_PATH_PREFIX)) {
    const trimmedName = playlistName?.trim();
    return titleFor(trimmedName ? trimmedName : GENERIC_PLAYLIST_TITLE);
  }

  const route = TAB_ROUTES.find((candidate) => matchesRoute(pathname, candidate));
  if (!route) return APP_NAME;

  return titleFor(TAB_ROUTE_INFO_BY_ROUTE[route].label);
}
