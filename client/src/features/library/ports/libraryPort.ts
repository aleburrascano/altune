import type {
  deleteTrack,
  getAllTracks,
  getLibraryAlbums,
  getLibraryArtists,
  getPlaylist,
  getPlaylists,
  getTracks,
  listTracksFeaturing,
  reacquireTrack,
  searchDiscovery,
} from '@shared/apiClient';
import type { LibraryWarn } from '@features/library/core/libraryFailure';

export interface LibraryPort {
  warn: LibraryWarn;
  getTracks: typeof getTracks;
  getAllTracks: typeof getAllTracks;
  listTracksFeaturing: typeof listTracksFeaturing;
  deleteTrack: typeof deleteTrack;
  reacquireTrack: typeof reacquireTrack;
  getLibraryAlbums: typeof getLibraryAlbums;
  getLibraryArtists: typeof getLibraryArtists;
  getPlaylists: typeof getPlaylists;
  getPlaylist: typeof getPlaylist;
  searchDiscovery: typeof searchDiscovery;
}
