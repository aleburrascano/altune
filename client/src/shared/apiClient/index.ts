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
} from '@shared/apiClient/adapters/tracks';
export { CORRELATION_HEADER, newCorrelationId } from '@shared/apiClient/adapters/correlationId';
export type {
  QueueSourceWire,
  QueueStateCurrentTrack,
  QueueStateResponse,
  SaveQueueStateRequest,
} from '@shared/apiClient/adapters/playback';
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
  UnreleasedAudioLink,
  UnreleasedSearchHit,
  UnreleasedTrack,
  UnreleasedTracksResponse,
  UnreleasedVersion,
} from '@shared/apiClient/adapters/enrichment';
export type {
  AlbumGroup,
  ArtistGroup,
  LibraryQuery,
  LibrarySort,
  ListAlbumsResponse,
  ListArtistsResponse,
} from '@shared/apiClient/adapters/library';
export type {
  Favorite,
  FavoriteRef,
  FavoriteTarget,
  FavoritesResponse,
} from '@shared/apiClient/adapters/favorites';
export {
  audioRequestHeaders,
  audioStreamUrl,
  fetchAudioUrls,
  recoverAudio,
} from '@shared/apiClient/adapters/audio';
export type { ResolvedAudioUrl } from '@shared/apiClient/adapters/audio';
export type { LyricsResponse, SyncedLine } from '@shared/apiClient/adapters/lyrics';
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
} from '@shared/apiClient/core/wireDecoders';
export type { BackfillFeaturedResult } from '@shared/apiClient/adapters/tracks';
export {
  getAlbumTracks,
  getArtistContent,
  getDeezerEnrichment,
  getEnrichment,
  getRelatedTracks,
  getUnreleasedTracks,
  searchUnreleasedTracks,
} from '@shared/apiClient/adapters/enrichment';
export { addFavorite, listFavorites, removeFavorite } from '@shared/apiClient/adapters/favorites';
export { getLibraryAlbums, getLibraryArtists } from '@shared/apiClient/adapters/library';
export { getLyrics } from '@shared/apiClient/adapters/lyrics';
export { getQueueState, saveQueueState } from '@shared/apiClient/adapters/playback';
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
