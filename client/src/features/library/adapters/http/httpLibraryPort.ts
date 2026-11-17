import {
  searchDiscovery,
  getLibraryAlbums,
  getLibraryArtists,
  getPlaylist,
  getPlaylists,
  deleteTrack,
  getAllTracks,
  getTracks,
  listTracksFeaturing,
  reacquireTrack,
} from '@shared/apiClient';
import { confirmDestructive, showFailureAlert } from '@shared/ui';
import type { LibraryPort } from '@features/library/ports/libraryPort';

export const httpLibraryPort: LibraryPort = {
  warn: (message, fields) => console.warn(message, fields),
  showFailure: (alert) => showFailureAlert(alert),
  confirmDestructive: (options) => confirmDestructive(options),
  getTracks: (...args) => getTracks(...args),
  getAllTracks: (...args) => getAllTracks(...args),
  listTracksFeaturing: (...args) => listTracksFeaturing(...args),
  deleteTrack: (...args) => deleteTrack(...args),
  reacquireTrack: (...args) => reacquireTrack(...args),
  getLibraryAlbums: (...args) => getLibraryAlbums(...args),
  getLibraryArtists: (...args) => getLibraryArtists(...args),
  getPlaylists: (...args) => getPlaylists(...args),
  getPlaylist: (...args) => getPlaylist(...args),
  searchDiscovery: (...args) => searchDiscovery(...args),
};
