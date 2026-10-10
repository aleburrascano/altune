import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  getPlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/apiClient/playlists';

import type { PlaylistsPort } from '@shared/playlists/ports/playlistsPort';

export const httpPlaylistsPort: PlaylistsPort = {
  getPlaylists: (...args) => getPlaylists(...args),
  getPlaylist: (...args) => getPlaylist(...args),
  createPlaylist: (...args) => createPlaylist(...args),
  renamePlaylist: (...args) => renamePlaylist(...args),
  deletePlaylist: (...args) => deletePlaylist(...args),
  addTracksToPlaylist: (...args) => addTracksToPlaylist(...args),
  removeTracksFromPlaylist: (...args) => removeTracksFromPlaylist(...args),
};
