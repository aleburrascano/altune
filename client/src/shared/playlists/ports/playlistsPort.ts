import type * as apiClient from '@shared/apiClient';

export interface PlaylistsPort {
  getPlaylists: typeof apiClient.getPlaylists;
  getPlaylist: typeof apiClient.getPlaylist;
  createPlaylist: typeof apiClient.createPlaylist;
  renamePlaylist: typeof apiClient.renamePlaylist;
  deletePlaylist: typeof apiClient.deletePlaylist;
  addTracksToPlaylist: typeof apiClient.addTracksToPlaylist;
  removeTracksFromPlaylist: typeof apiClient.removeTracksFromPlaylist;
}
