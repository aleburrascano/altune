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
} from '@shared/catalog/core/ids';
export type { FavoriteKey, PlaylistId, TrackId } from '@shared/catalog/core/ids';
export { ACQUISITION_STATUSES } from '@shared/catalog/core/trackResponse';
export type {
  AcquisitionStatus,
  CatalogSourceRef,
  CreateTrackRequest,
  FailedAcquisition,
  FeaturedArtist,
  ListTracksResponse,
  TrackAcquisition,
  TrackFields,
  TrackResponse,
} from '@shared/catalog/core/trackResponse';
export type {
  AddTracksToPlaylistRequest,
  AddTracksToPlaylistResponse,
  CreatePlaylistRequest,
  ListPlaylistsResponse,
  PlaylistDetailResponse,
  PlaylistResponse,
  RemoveTracksFromPlaylistRequest,
  RemoveTracksFromPlaylistResponse,
} from '@shared/catalog/core/playlistResponse';
export {
  acquisitionOf,
  toFailed,
  toPending,
  toReady,
  toTrackStatus,
} from '@shared/catalog/core/trackAcquisition';
export type { AcquisitionTransition, WireTrackStatus } from '@shared/catalog/core/trackAcquisition';
export { featuredArtistsFromExtras, withFeaturing } from '@shared/catalog/core/featuredArtists';
export { trackToDiscoveryResult } from '@shared/catalog/core/trackToDiscoveryResult';
export {
  DISCOVERY_CONFIDENCES,
  DISCOVERY_KINDS,
  PROVIDER_STATUSES,
} from '@shared/catalog/core/discoveryResult';
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
  RelatedGroup,
  ResultSection,
  SearchHistoryItem,
} from '@shared/catalog/core/discoveryResult';
