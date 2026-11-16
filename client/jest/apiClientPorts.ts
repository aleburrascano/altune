import {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  fetchAudioUrls,
  getPlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
  retryAcquisition,
} from '@shared/apiClient';
import type { AcquisitionPort } from '@shared/acquisition';
import type { OfflinePort } from '@shared/offline';
import type { PlaylistsPort } from '@shared/playlists';

export const apiClientAcquisitionPort: AcquisitionPort = {
  retryAcquisition: (...args) => retryAcquisition(...args),
};

export const apiClientPlaylistsPort: PlaylistsPort = {
  getPlaylists: (...args) => getPlaylists(...args),
  getPlaylist: (...args) => getPlaylist(...args),
  createPlaylist: (...args) => createPlaylist(...args),
  renamePlaylist: (...args) => renamePlaylist(...args),
  deletePlaylist: (...args) => deletePlaylist(...args),
  addTracksToPlaylist: (...args) => addTracksToPlaylist(...args),
  removeTracksFromPlaylist: (...args) => removeTracksFromPlaylist(...args),
};

export const apiClientOfflinePort: OfflinePort = {
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
};
