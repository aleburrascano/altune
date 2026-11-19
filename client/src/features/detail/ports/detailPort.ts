import type * as apiClient from '@shared/apiClient';

export interface DetailPort {
  searchDiscovery: typeof apiClient.searchDiscovery;
  getEnrichment: typeof apiClient.getEnrichment;
  getDeezerEnrichment: typeof apiClient.getDeezerEnrichment;
  getAlbumTracks: typeof apiClient.getAlbumTracks;
  getRelatedTracks: typeof apiClient.getRelatedTracks;
  getArtistContent: typeof apiClient.getArtistContent;
  getUnreleasedTracks: typeof apiClient.getUnreleasedTracks;
  createTrack: typeof apiClient.createTrack;
  getTracks: typeof apiClient.getTracks;
  getLibraryAlbums: typeof apiClient.getLibraryAlbums;
  warn(message: string, detail: Record<string, unknown>): void;
}
