import type * as discovery from '@shared/apiClient/discovery';
import type * as library from '@shared/apiClient/library';
import type * as playlists from '@shared/apiClient/playlists';
import type * as tracks from '@shared/apiClient/tracks';

export interface LibraryPort {
  getTracks: typeof tracks.getTracks;
  getAllTracks: typeof tracks.getAllTracks;
  listTracksFeaturing: typeof tracks.listTracksFeaturing;
  deleteTrack: typeof tracks.deleteTrack;
  reacquireTrack: typeof tracks.reacquireTrack;
  getLibraryAlbums: typeof library.getLibraryAlbums;
  getLibraryArtists: typeof library.getLibraryArtists;
  getPlaylists: typeof playlists.getPlaylists;
  getPlaylist: typeof playlists.getPlaylist;
  searchDiscovery: typeof discovery.searchDiscovery;
}
