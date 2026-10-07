import type * as discovery from '@shared/api-client/discovery';
import type * as enrichment from '@shared/api-client/enrichment';
import type * as library from '@shared/api-client/library';
import type * as tracks from '@shared/api-client/tracks';

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
