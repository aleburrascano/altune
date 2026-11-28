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
import { warn } from '@shared/log';
import { confirmDestructive, showFailureAlert } from '@shared/ui';
import { toAlbumsPage, toArtistsPage } from '@features/library/adapters/http/mapping';
import type { LibraryPort } from '@features/library/ports/ports';

export const httpLibraryPort: LibraryPort = {
  warn: (message, fields) => warn(message, fields),
  showFailure: (alert) => showFailureAlert(alert),
  confirmDestructive: (options) => confirmDestructive(options),
  getTracks: (...args) => getTracks(...args),
  getAllTracks: (...args) => getAllTracks(...args),
  listTracksFeaturing: (...args) => listTracksFeaturing(...args),
  deleteTrack: (...args) => deleteTrack(...args),
  reacquireTrack: (...args) => reacquireTrack(...args),
  getLibraryAlbums: async (query, signal) => toAlbumsPage(await getLibraryAlbums(query, signal)),
  getLibraryArtists: async (query, signal) => toArtistsPage(await getLibraryArtists(query, signal)),
  getPlaylists: (...args) => getPlaylists(...args),
  getPlaylist: (...args) => getPlaylist(...args),
  searchDiscovery: (...args) => searchDiscovery(...args),
};
