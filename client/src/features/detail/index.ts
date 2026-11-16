export { DetailScreen } from '@features/detail/ui/DetailScreen';
export { UnreleasedAllScreen } from '@features/detail/ui/unreleased/ArtistUnreleasedAll';
export { UnreleasedSearchHits } from '@features/detail/view/unreleased/UnreleasedSearchHits';
export { DetailPortProvider, useDetailPort } from '@features/detail/view/DetailPortProvider';
export { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
export type { DetailPort } from '@features/detail/ports/detailPort';
export { AlbumDetailBody } from '@features/detail/view/AlbumDetailBody';
export { AlbumMoreTracks } from '@features/detail/view/AlbumMoreTracks';
export { AlbumRail } from '@features/detail/view/AlbumRail';
export type { AlbumDetailState } from '@features/detail/view/useAlbumDetailState';
export {
  ownedTrack,
  playButtonState,
  splitOwned,
  toPlaybackQueue,
} from '@features/detail/domain/ownedTrack';
export { AlbumTrackList } from '@features/detail/view/AlbumTrackList';
export { ArtistDetailBody } from '@features/detail/view/ArtistDetailBody';
export { ArtistUnreleased } from '@features/detail/view/unreleased/ArtistUnreleased';
export { CollapsibleSectionHeader } from '@features/detail/view/CollapsibleSectionHeader';
export {
  contentFailure,
  failFastOnSettledContentFailure,
} from '@features/detail/domain/contentStatus';
export { buildAlbumFacts } from '@features/detail/ui/buildDetailFacts';
export {
  _resetDetailHealthForTest,
  fetchTallyingOutcome,
  flushDetailHealth,
  recordContentFetchOutcome,
} from '@features/detail/domain/detailHealth';
export { useAlbumTracks } from '@features/detail/core/useAlbumTracks';
export { useDetailEnrichments } from '@features/detail/hooks/useDetailEnrichments';
export { useArtistContent } from '@features/detail/core/useArtistContent';
export {
  createMemoryDetailPort,
  recordWarnings,
} from '@features/detail/__tests__/memoryDetailPort';
export { DetailBodyLayout, DetailScaffold } from '@features/detail/view/DetailScaffold';
export { DiscographySections } from '@features/detail/view/DiscographySections';
export { AlbumCardsSkeleton } from '@features/detail/view/DetailSkeleton';
export { RelatedTracksSection } from '@features/detail/view/RelatedTracksSection';
export { gridCellWidthFor } from '@features/detail/view/detailStyles';
export {
  albumYear,
  compactCount,
  formatRuntime,
  trackSubtitleWithFeaturing,
} from '@features/detail/ui/formatters';
export { LastFmEnrichmentSection } from '@features/detail/view/LastFmEnrichmentSection';
export {
  detailRouteFor,
  featuringHref,
  openDetail,
} from '@features/detail/domain/detailNavigation';
export {
  useLateralNav,
  useOpenDetail,
  useOpenFeaturing,
} from '@features/detail/hooks/useDetailNavigation';
export { normalizeForCompare } from '@features/detail/domain/normalizeForCompare';
export {
  albumExtras,
  extractFeaturedFromText,
  resolveFeatured,
  trackExtras,
} from '@features/detail/domain/trackExtras';
export type { OwnedTrack } from '@features/detail/domain/ownedTrack';
export { isResultPlaying, resolvePlaySource } from '@features/detail/domain/playSource';
export { resolveEntityQuery } from '@features/detail/domain/resolveEntityQuery';
export {
  SAVE_ALL_CONCURRENCY,
  isOptimisticTrackId,
  optimisticTrack,
  optimisticTrackId,
  ownedRetryTrackId,
  saveControlInteractive,
  saveControlLabel,
  saveControlState,
  saveControlText,
  saveDisplayState,
  saveIdempotencyKey,
  toCreateTrackRequest,
} from '@features/detail/domain/saveTrack';
export type { SaveControlState, SaveState } from '@features/detail/domain/saveTrack';
export { SectionError } from '@features/detail/view/Section';
export { isAcquirable, unreleasedAsResult } from '@features/detail/domain/unreleasedTrack';
export { unreleasedView } from '@features/detail/domain/unreleasedView';
export { useAlbumDetailState } from '@features/detail/view/useAlbumDetailState';
export type {
  DetailWarning,
  RecordingDetailPort,
  MemoryDetailSeed,
} from '@features/detail/__tests__/memoryDetailPort';
export { useAlbumSaveAll } from '@features/detail/view/useAlbumSaveAll';
export { useRelatedTracks } from '@features/detail/hooks/useRelatedTracks';
export { useAlbumDiscovery, useArtistDiscovery } from '@features/detail/view/useEntityDiscovery';
export type { EnrichmentProvider } from '@features/detail/domain/detailHealth';
export { useResolveMissingSources } from '@features/detail/view/useResolveMissingSources';
export { useDiscographyFilter } from '@features/detail/view/useDiscographyFilter';
export {
  useLibraryAlbumsForArtist,
  useLibraryTracksForAlbum,
  useLibraryTracksForArtist,
} from '@features/detail/hooks/useLibraryMatches';
export { useOwnedPlayback } from '@features/detail/hooks/useOwnedPlayback';
export type { OwnedPlaybackContext } from '@features/detail/hooks/useOwnedPlayback';
export { useSaveTrack, useTrackSave } from '@features/detail/view/useSaveTrack';
export type { SaveTrack } from '@features/detail/view/useSaveTrack';
export { DetailHandoffProvider } from '@features/detail/view/useDetailHandoff';
export { useReportWrongAlbum } from '@features/detail/view/useReportWrongAlbum';
export { useRetryTrack } from '@features/detail/hooks/useRetryTrack';
export { SaveAllPill, TrackSaveControl, TrackSavePill } from '@features/detail/view/SaveControls';
export { useResolvedOwnedTrack } from '@features/detail/core/useOwnedTrack';
export type { TrackIdentity } from '@features/detail/core/useOwnedTrack';
export type { LateralNavHandle } from '@features/detail/view/useTrackDetailActions';
export { TrackDetailBody } from '@features/detail/view/TrackDetailBody';
