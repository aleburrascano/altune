import { searchDiscovery } from '@shared/apiClient/discovery';
import { getLibraryAlbums, getLibraryArtists } from '@shared/apiClient/library';
import { getPlaylist, getPlaylists } from '@shared/apiClient/playlists';
import {
  deleteTrack,
  getAllTracks,
  getTracks,
  listTracksFeaturing,
  reacquireTrack,
} from '@shared/apiClient/tracks';

import type { LibraryPort } from '@features/library/ports/libraryPort';

export const httpLibraryPort: LibraryPort = {
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
