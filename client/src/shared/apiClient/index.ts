export { ApiError, NetworkError } from '@shared/failure';
export { apiFetch } from '@shared/apiClient/adapters/http';

export {
  NO_PLAYLIST_ID,
  asFavoriteKey,
  asPlaylistId,
  asTrackId,
  idPathSegment,
  isSafeId,
  parsePlaylistId,
  parseTrackId,
  sourceIdList,
} from '@shared/apiClient/ids';
export type {
  AcquisitionStatus,
  CatalogSourceRef,
  CreateTrackRequest,
  FailedAcquisition,
  FeaturedArtist,
  ListTracksResponse,
  TrackFields,
  TrackResponse,
} from '@shared/apiClient/trackResponse';
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
export type { PlaylistId, TrackId } from '@shared/apiClient/ids';
export {
  acquisitionOf,
  toFailed,
  toPending,
  toReady,
  toTrackStatus,
} from '@shared/apiClient/trackAcquisition';
export type {
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
} from '@shared/apiClient/playlistResponse';
export { CORRELATION_HEADER, newCorrelationId } from '@shared/apiClient/correlationId';
export type { AcquisitionTransition, WireTrackStatus } from '@shared/apiClient/trackAcquisition';
export type {
  DiscoveryKind,
  DiscoveryProviderInfo,
  DiscoveryProviderStatus,
  DiscoveryResult,
  DiscoverySearchHistoryResponse,
  DiscoverySearchResponse,
  DiscoverySource,
  DiscoverySuggestResponse,
  DiscoverySuggestion,
  ResultSection,
  SearchHistoryItem,
} from '@shared/apiClient/discovery';
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
export { featuredArtistsFromExtras, withFeaturing } from '@shared/apiClient/featuredArtists';
export { trackToDiscoveryResult } from '@shared/apiClient/trackToDiscoveryResult';
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
export { ACQUISITION_STATUSES } from '@shared/apiClient/trackResponse';
