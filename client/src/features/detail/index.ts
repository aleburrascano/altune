export { DetailScreen } from '@features/detail/ui/DetailScreen';
export { UnreleasedAllScreen } from '@features/detail/ui/unreleased/ArtistUnreleasedAll';
export { UnreleasedSearchHits } from '@features/detail/ui/unreleased/UnreleasedSearchHits';
export { DetailPortProvider, useDetailPort } from '@features/detail/ports/DetailPortProvider';
export { httpDetailPort } from '@features/detail/adapters/http/httpDetailPort';
export type { DetailPort } from '@features/detail/ports/detailPort';
export { AlbumDetailBody } from '@features/detail/ui/AlbumDetailBody';
export { AlbumMoreTracks } from '@features/detail/ui/AlbumMoreTracks';
export { AlbumRail } from '@features/detail/ui/AlbumRail';
export type { AlbumDetailState } from '@features/detail/hooks/useAlbumDetailState';
export {
  ownedTrack,
  playButtonState,
  splitOwned,
  toPlaybackQueue,
} from '@features/detail/domain/ownedTrack';
export { AlbumTrackList } from '@features/detail/ui/AlbumTrackList';
export { ArtistDetailBody } from '@features/detail/ui/ArtistDetailBody';
export { ArtistUnreleased } from '@features/detail/ui/unreleased/ArtistUnreleased';
export { CollapsibleSectionHeader } from '@features/detail/ui/CollapsibleSectionHeader';
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
export { useAlbumTracks } from '@features/detail/hooks/useAlbumTracks';
export { useDetailEnrichments } from '@features/detail/hooks/useDetailEnrichments';
export { useArtistContent } from '@features/detail/hooks/useArtistContent';
export { createMemoryDetailPort } from '@features/detail/__tests__/memoryDetailPort';
export { DetailBodyLayout, DetailScaffold } from '@features/detail/ui/DetailScaffold';
export { DiscographySections } from '@features/detail/ui/DiscographySections';
export { AlbumCardsSkeleton } from '@features/detail/ui/DetailSkeleton';
export { RelatedTracksSection } from '@features/detail/ui/RelatedTracksSection';
export { gridCellWidthFor } from '@features/detail/ui/detailStyles';
export {
  albumYear,
  compactCount,
  formatRuntime,
  trackSubtitleWithFeaturing,
} from '@features/detail/ui/formatters';
export { LastFmEnrichmentSection } from '@features/detail/ui/LastFmEnrichmentSection';
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
export { SectionError } from '@features/detail/ui/Section';
export { isAcquirable, unreleasedAsResult } from '@features/detail/domain/unreleasedTrack';
export { unreleasedView } from '@features/detail/domain/unreleasedView';
export { useAlbumDetailState } from '@features/detail/hooks/useAlbumDetailState';
export type { MemoryDetailSeed } from '@features/detail/__tests__/memoryDetailPort';
export { useAlbumSaveAll } from '@features/detail/hooks/useAlbumSaveAll';
export { useRelatedTracks } from '@features/detail/hooks/useRelatedTracks';
export { useAlbumDiscovery, useArtistDiscovery } from '@features/detail/hooks/useEntityDiscovery';
export type { EnrichmentProvider } from '@features/detail/domain/detailHealth';
export { useResolveMissingSources } from '@features/detail/hooks/useResolveMissingSources';
export { useDiscographyFilter } from '@features/detail/hooks/useDiscographyFilter';
export {
  useLibraryAlbumsForArtist,
  useLibraryTracksForAlbum,
  useLibraryTracksForArtist,
} from '@features/detail/hooks/useLibraryMatches';
export { useOwnedPlayback } from '@features/detail/hooks/useOwnedPlayback';
export type { OwnedPlaybackContext } from '@features/detail/hooks/useOwnedPlayback';
export { useSaveTrack, useTrackSave } from '@features/detail/hooks/useSaveTrack';
export type { SaveTrack } from '@features/detail/hooks/useSaveTrack';
export { DetailHandoffProvider } from '@features/detail/hooks/useDetailHandoff';
export { useReportWrongAlbum } from '@features/detail/hooks/useReportWrongAlbum';
export { useRetryTrack } from '@features/detail/hooks/useRetryTrack';
export { SaveAllPill, TrackSaveControl, TrackSavePill } from '@features/detail/ui/SaveControls';
export { useResolvedOwnedTrack } from '@features/detail/hooks/useOwnedTrack';
export type { TrackIdentity } from '@features/detail/hooks/useOwnedTrack';
export type { LateralNavHandle } from '@features/detail/hooks/useTrackDetailActions';
export { TrackDetailBody } from '@features/detail/ui/TrackDetailBody';
