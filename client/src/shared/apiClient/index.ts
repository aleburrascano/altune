export { ApiError, NetworkError } from '@shared/failure';
export { apiFetch } from '@shared/apiClient/adapters/http';

export {
  backfillFeaturedArtists,
  createTrack,
  deleteTrack,
  getAllTracks,
  getTracks,
  listTracksFeaturing,
  parseTrackResponse,
  reacquireTrack,
  retryAcquisition,
  tryParseTrackResponse,
} from '@shared/apiClient/tracks';
export { CORRELATION_HEADER, newCorrelationId } from '@shared/apiClient/correlationId';
export type {
  QueueSourceWire,
  QueueStateCurrentTrack,
  QueueStateResponse,
  SaveQueueStateRequest,
} from '@shared/apiClient/playback';
export {
  clearSearchHistory,
  listSearchHistory,
  recordSearchHistory,
  searchDiscovery,
  suggestDiscovery,
} from '@shared/apiClient/discovery';
export type {
  ArtistContentOptions,
  ArtistContentResponse,
  ContentFetchResponse,
  DeezerEnrichmentResponse,
  EnrichmentResponse,
  LastFmEnrichmentResponse,
  UnreleasedAudioLink,
  UnreleasedSearchHit,
  UnreleasedTrack,
  UnreleasedTracksResponse,
  UnreleasedVersion,
} from '@shared/apiClient/enrichment';
export type {
  AlbumGroup,
  ArtistGroup,
  LibraryQuery,
  LibrarySort,
  ListAlbumsResponse,
  ListArtistsResponse,
} from '@shared/apiClient/library';
export type {
  Favorite,
  FavoriteRef,
  FavoriteTarget,
  FavoritesResponse,
} from '@shared/apiClient/favorites';
export {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  isAudioPrefetchEnabled,
  recoverAudio,
} from '@shared/apiClient/audio';
export type { ResolvedAudioUrl } from '@shared/apiClient/audio';
export type { LyricsResponse, SyncedLine } from '@shared/apiClient/lyrics';
export {
  asArray,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableBoolean,
  nullableNumber,
  nullableString,
  parseErrorBody,
} from '@shared/apiClient/wireDecoders';
export type { BackfillFeaturedResult } from '@shared/apiClient/tracks';
export {
  getAlbumTracks,
  getArtistContent,
  getDeezerEnrichment,
  getEnrichment,
  getLastFmEnrichment,
  getRelatedTracks,
  getUnreleasedTracks,
  searchUnreleasedTracks,
} from '@shared/apiClient/enrichment';
export { addFavorite, listFavorites, removeFavorite } from '@shared/apiClient/favorites';
export { getLibraryAlbums, getLibraryArtists } from '@shared/apiClient/library';
export { getLyrics } from '@shared/apiClient/lyrics';
export { getQueueState, saveQueueState } from '@shared/apiClient/playback';
export {
  addTracksToPlaylist,
  createPlaylist,
  deletePlaylist,
  getPlaylist,
  getPlaylists,
  removeTracksFromPlaylist,
  renamePlaylist,
} from '@shared/apiClient/playlists';
export type { PlaylistPage } from '@shared/apiClient/playlists';
