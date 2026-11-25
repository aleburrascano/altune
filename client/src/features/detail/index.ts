export { DetailScreen } from '@features/detail/view/DetailScreen';
export { UnreleasedAllScreen } from '@features/detail/view/unreleased/ArtistUnreleasedAll';
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
} from '@features/detail/core/ownedTrack';
export { AlbumTrackList } from '@features/detail/view/AlbumTrackList';
export { ArtistDetailBody } from '@features/detail/view/ArtistDetailBody';
export { ArtistUnreleased } from '@features/detail/view/unreleased/ArtistUnreleased';
export { CollapsibleSectionHeader } from '@features/detail/view/CollapsibleSectionHeader';
export {
  contentFailure,
  failFastOnSettledContentFailure,
} from '@features/detail/core/contentStatus';
export { buildAlbumFacts } from '@features/detail/core/buildDetailFacts';
export {
  _resetDetailHealthForTest,
  fetchTallyingOutcome,
  flushDetailHealth,
  recordContentFetchOutcome,
} from '@features/detail/core/detailHealth';
export { useAlbumTracks } from '@features/detail/view/useAlbumTracks';
export { useDetailEnrichments } from '@features/detail/view/useDetailEnrichments';
export { useArtistContent } from '@features/detail/view/useArtistContent';
export { DetailBodyLayout, DetailScaffold } from '@features/detail/view/DetailScaffold';
export { DiscographySections } from '@features/detail/view/DiscographySections';
export { AlbumCardsSkeleton } from '@features/detail/view/DetailSkeleton';
export { RelatedTracksSection } from '@features/detail/view/RelatedTracksSection';
export { gridCellWidthFor } from '@features/detail/view/detailStyles';
export {
  albumYear,
  formatRuntime,
  trackSubtitleWithFeaturing,
} from '@features/detail/core/formatters';
export { DetailNavigatorProvider } from '@features/detail/view/DetailNavigatorProvider';
export { detailRouteFor, featuringHref, openDetail } from '@features/detail/core/detailNavigation';
export {
  useLateralNav,
  useOpenDetail,
  useOpenFeaturing,
} from '@features/detail/view/useDetailNavigation';
export { normalizeForCompare } from '@features/detail/core/normalizeForCompare';
export {
  albumExtras,
  extractFeaturedFromText,
  resolveFeatured,
  trackExtras,
} from '@features/detail/core/trackExtras';
export type { OwnedTrack } from '@features/detail/core/ownedTrack';
export { isResultPlaying, resolvePlaySource } from '@features/detail/core/playSource';
export { resolveEntityQuery } from '@features/detail/core/resolveEntityQuery';
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
} from '@features/detail/core/saveTrack';
export type { SaveControlState, SaveState } from '@features/detail/core/saveTrack';
export { SectionError } from '@features/detail/view/Section';
export { isAcquirable, unreleasedAsResult } from '@features/detail/core/unreleasedTrack';
export { unreleasedView } from '@features/detail/core/unreleasedView';
export { useAlbumDetailState } from '@features/detail/view/useAlbumDetailState';
export { useAlbumSaveAll } from '@features/detail/view/useAlbumSaveAll';
export { useRelatedTracks } from '@features/detail/view/useRelatedTracks';
export { useAlbumDiscovery, useArtistDiscovery } from '@features/detail/view/useEntityDiscovery';
export { useResolveMissingSources } from '@features/detail/view/useResolveMissingSources';
export { useDiscographyFilter } from '@features/detail/view/useDiscographyFilter';
export {
  useLibraryAlbumsForArtist,
  useLibraryTracksForAlbum,
  useLibraryTracksForArtist,
} from '@features/detail/view/useLibraryMatches';
export { useOwnedPlayback } from '@features/detail/view/useOwnedPlayback';
export type { OwnedPlaybackContext } from '@features/detail/view/useOwnedPlayback';
export { useSaveTrack, useTrackSave } from '@features/detail/view/useSaveTrack';
export type { SaveTrack } from '@features/detail/view/useSaveTrack';
export { DetailHandoffProvider } from '@features/detail/view/useDetailHandoff';
export { useReportWrongAlbum } from '@features/detail/view/useReportWrongAlbum';
export { useRetryTrack } from '@features/detail/view/useRetryTrack';
export { SaveAllPill, TrackSaveControl, TrackSavePill } from '@features/detail/view/SaveControls';
export { useResolvedOwnedTrack } from '@features/detail/core/useOwnedTrack';
export type { TrackIdentity } from '@features/detail/core/useOwnedTrack';
export type { LateralNavHandle } from '@features/detail/view/useTrackDetailActions';
export { TrackDetailBody } from '@features/detail/view/TrackDetailBody';
