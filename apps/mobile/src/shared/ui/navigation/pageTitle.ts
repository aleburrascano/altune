import { TAB_ROUTE_INFO_BY_ROUTE, TAB_ROUTES } from './tabRoutes';

const APP_NAME = 'Altune';
const GENERIC_PLAYLIST_TITLE = 'Playlist';
const PLAYLIST_PATH_PREFIX = '/library/playlist/';

function titleFor(label: string): string {
  return `${label} · ${APP_NAME}`;
}

export function pageTitleFor(pathname: string, playlistName?: string | null): string {
  if (pathname.startsWith(PLAYLIST_PATH_PREFIX)) {
    const trimmedName = playlistName?.trim();
    return titleFor(trimmedName ? trimmedName : GENERIC_PLAYLIST_TITLE);
  }

  const route = TAB_ROUTES.find((candidate) => pathname.startsWith(`/${candidate}`));
  if (!route) return APP_NAME;

  return titleFor(TAB_ROUTE_INFO_BY_ROUTE[route].label);
}
