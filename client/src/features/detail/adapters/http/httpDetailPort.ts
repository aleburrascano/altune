import { searchDiscovery } from '@shared/api-client/discovery';
import {
  getAlbumTracks,
  getArtistContent,
  getDeezerEnrichment,
  getEnrichment,
  getLastFmEnrichment,
  getRelatedTracks,
} from '@shared/api-client/enrichment';
import { getLibraryAlbums } from '@shared/api-client/library';
import { createTrack, getTracks } from '@shared/api-client/tracks';

import type { DetailPort } from '@features/detail/ports/detailPort';

export const httpDetailPort: DetailPort = {
  searchDiscovery: (...args) => searchDiscovery(...args),
  getEnrichment: (...args) => getEnrichment(...args),
  getLastFmEnrichment: (...args) => getLastFmEnrichment(...args),
  getDeezerEnrichment: (...args) => getDeezerEnrichment(...args),
  getAlbumTracks: (...args) => getAlbumTracks(...args),
  getRelatedTracks: (...args) => getRelatedTracks(...args),
  getArtistContent: (...args) => getArtistContent(...args),
  createTrack: (...args) => createTrack(...args),
  getTracks: (...args) => getTracks(...args),
  getLibraryAlbums: (...args) => getLibraryAlbums(...args),
};
