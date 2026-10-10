import type * as discovery from '@shared/apiClient/discovery';
import type * as enrichment from '@shared/apiClient/enrichment';
import type * as library from '@shared/apiClient/library';
import type * as tracks from '@shared/apiClient/tracks';

export interface DetailPort {
  searchDiscovery: typeof discovery.searchDiscovery;
  getEnrichment: typeof enrichment.getEnrichment;
  getLastFmEnrichment: typeof enrichment.getLastFmEnrichment;
  getDeezerEnrichment: typeof enrichment.getDeezerEnrichment;
  getAlbumTracks: typeof enrichment.getAlbumTracks;
  getRelatedTracks: typeof enrichment.getRelatedTracks;
  getArtistContent: typeof enrichment.getArtistContent;
  createTrack: typeof tracks.createTrack;
  getTracks: typeof tracks.getTracks;
  getLibraryAlbums: typeof library.getLibraryAlbums;
}
