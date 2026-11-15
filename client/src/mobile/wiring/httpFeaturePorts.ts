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
import { httpDetailPort, type DetailPort } from '@features/detail';
import {
  httpDiscoverPort,
  httpFavoritesPort,
  type DiscoverPort,
  type FavoritesPort,
} from '@features/discovery';
import { httpLibraryPort, type LibraryPort } from '@features/library';
import { httpSettingsPort, type SettingsPort } from '@features/settings';

export interface FeaturePortSet {
  acquisition: AcquisitionPort;
  detail: DetailPort;
  discover: DiscoverPort;
  favorites: FavoritesPort;
  library: LibraryPort;
  playlists: PlaylistsPort;
  settings: SettingsPort;
}

export const httpFeaturePorts: FeaturePortSet = {
  acquisition: {
    retryAcquisition: (...args) => retryAcquisition(...args),
  },
  detail: httpDetailPort,
  discover: httpDiscoverPort,
  favorites: httpFavoritesPort,
  library: httpLibraryPort,
  playlists: {
    getPlaylists: (...args) => getPlaylists(...args),
    getPlaylist: (...args) => getPlaylist(...args),
    createPlaylist: (...args) => createPlaylist(...args),
    renamePlaylist: (...args) => renamePlaylist(...args),
    deletePlaylist: (...args) => deletePlaylist(...args),
    addTracksToPlaylist: (...args) => addTracksToPlaylist(...args),
    removeTracksFromPlaylist: (...args) => removeTracksFromPlaylist(...args),
  },
  settings: httpSettingsPort,
};

export const httpOfflinePort: OfflinePort = {
  fetchAudioUrls: (...args) => fetchAudioUrls(...args),
};
