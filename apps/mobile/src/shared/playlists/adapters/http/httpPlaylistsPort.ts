import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/api-client/playlists';

import type { PlaylistsPort } from '../../ports/playlistsPort';

export const httpPlaylistsPort: PlaylistsPort = {
  getPlaylists: (...args) => getPlaylists(...args),
  createPlaylist: (...args) => createPlaylist(...args),
  renamePlaylist: (...args) => renamePlaylist(...args),
  deletePlaylist: (...args) => deletePlaylist(...args),
  addTracksToPlaylist: (...args) => addTracksToPlaylist(...args),
  removeTracksFromPlaylist: (...args) => removeTracksFromPlaylist(...args),
};
