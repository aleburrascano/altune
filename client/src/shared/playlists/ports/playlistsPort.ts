import type * as playlists from '@shared/api-client/playlists';

export interface PlaylistsPort {
  getPlaylists: typeof playlists.getPlaylists;
  getPlaylist: typeof playlists.getPlaylist;
  createPlaylist: typeof playlists.createPlaylist;
  renamePlaylist: typeof playlists.renamePlaylist;
  deletePlaylist: typeof playlists.deletePlaylist;
  addTracksToPlaylist: typeof playlists.addTracksToPlaylist;
  removeTracksFromPlaylist: typeof playlists.removeTracksFromPlaylist;
}
