import { pageTitleFor } from '@shared/ui';

const PLAYLIST_PATH_PATTERN = /^\/library\/playlist\/([^/]+)/;

export type NamedPlaylist = { id: string; name: string };

export function tabsTitleFor(pathname: string, playlists: readonly NamedPlaylist[]): string {
  const playlistId = PLAYLIST_PATH_PATTERN.exec(pathname)?.[1];
  const playlistName =
    playlistId === undefined
      ? undefined
      : playlists.find((playlist) => playlist.id === playlistId)?.name;
  return pageTitleFor(pathname, playlistName);
}
