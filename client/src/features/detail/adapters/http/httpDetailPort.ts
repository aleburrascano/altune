import {
  searchDiscovery,
  getAlbumTracks,
  getArtistContent,
  getDeezerEnrichment,
  getEnrichment,
  getLastFmEnrichment,
  getRelatedTracks,
  getUnreleasedTracks,
  getLibraryAlbums,
  createTrack,
  getTracks,
} from '@shared/apiClient';
import { warn } from '@shared/log';
import type { DetailPort } from '@features/detail/ports/detailPort';

export const httpDetailPort: DetailPort = {
  searchDiscovery: (...args) => searchDiscovery(...args),
  getEnrichment: (...args) => getEnrichment(...args),
  getLastFmEnrichment: (...args) => getLastFmEnrichment(...args),
  getDeezerEnrichment: (...args) => getDeezerEnrichment(...args),
  getAlbumTracks: (...args) => getAlbumTracks(...args),
  getRelatedTracks: (...args) => getRelatedTracks(...args),
  getArtistContent: (...args) => getArtistContent(...args),
  getUnreleasedTracks: (...args) => getUnreleasedTracks(...args),
  createTrack: (...args) => createTrack(...args),
  getTracks: (...args) => getTracks(...args),
  getLibraryAlbums: (...args) => getLibraryAlbums(...args),
  warn: (message, detail) => warn(message, detail),
};
