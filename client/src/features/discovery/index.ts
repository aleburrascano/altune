export { DiscoverScreen } from '@features/discovery/ui/search/DiscoverScreen';
export { registerSearchStateSignOutReset } from '@features/discovery/adapters/registerSearchStateSignOutReset';
export {
  DiscoverPortProvider,
  useDiscoverPort,
} from '@features/discovery/ports/DiscoverPortProvider';
export {
  FavoritesPortProvider,
  useFavoritesPort,
} from '@features/discovery/ports/FavoritesPortProvider';
export { UnreleasedHitsSlotProvider } from '@features/discovery/ports/UnreleasedHitsSlot';
export { httpDiscoverPort } from '@features/discovery/adapters/http/httpDiscoverPort';
export { httpFavoritesPort } from '@features/discovery/adapters/http/httpFavoritesPort';
export type { DiscoverPort } from '@features/discovery/ports/discoverPort';
export type { FavoritesPort } from '@features/discovery/ports/favoritesPort';
export { BlendedSection } from '@features/discovery/view/results/BlendedSection';
export { resultFixture } from '@features/discovery/__tests__/fixtures';
export type { ResultsCommonProps } from '@features/discovery/view/results/ResultsList';
export { buildImpressionRows } from '@features/discovery/domain/buildImpressionRows';
export { DiscoverBody } from '@features/discovery/view/search/DiscoverBody';
export type {
  DiscoverHookState,
  DiscoverView,
  SearchCorrection,
} from '@features/discovery/domain/discoverView';
export { createMemoryDiscoverPort } from '@features/discovery/__tests__/memoryDiscoverPort';
export {
  useAutocompleteSuggestions,
  useSuggestionVisibility,
} from '@features/discovery/hooks/history/useSuggestions';
export { useDiscoverSearch } from '@features/discovery/hooks/search/useDiscoverSearch';
export {
  useDiscoverClearSearchHistory,
  useSearchHistory,
} from '@features/discovery/hooks/history/useSearchHistory';
export { CorrectionBanner } from '@features/discovery/view/results/CorrectionBanner';
export { DiscoverRow } from '@features/discovery/ui/results/DiscoverRow';
export {
  asyncViewForDiscoverView,
  correctionForResponse,
  resultsIncompleteForState,
  searchAnnouncement,
  viewForState,
} from '@features/discovery/domain/discoverView';
export { createMemoryFavoritesPort } from '@features/discovery/__tests__/memoryFavoritesPort';
export { FilterChips } from '@features/discovery/view/results/FilterChips';
export { FilteredResults } from '@features/discovery/view/results/FilteredResults';
export {
  kindLabel,
  resultKey,
  resultSecondaryLine,
} from '@features/discovery/domain/resultDisplay';
export { ResultsList } from '@features/discovery/view/results/ResultsList';
export {
  MAX_QUERY_LENGTH,
  MAX_SEARCH_OFFSET,
  MAX_SEARCH_PAGES,
  SEARCH_PAGE_SIZE,
  clampQueryWords,
  isSearchableQuery,
  normalizeQuery,
  sliceCodePoints,
} from '@features/discovery/domain/searchPaging';
export {
  getSearchState,
  resetSearchState,
  setSearchState,
} from '@features/discovery/domain/searchState';
export { SectionGrid } from '@features/discovery/view/results/SectionGrid';
export { TopResultCard } from '@features/discovery/ui/results/TopResultCard';
export { stashHandoffForDetail } from '@features/discovery/domain/stashHandoffForDetail';
export { useDebouncedSearch } from '@features/discovery/view/search/useDebouncedSearch';
export { useDiscoverLogic } from '@features/discovery/hooks/search/useDiscoverLogic';
export { useFavorites } from '@features/discovery/hooks/favorites/useFavorites';
export { useImpressionLogger } from '@features/discovery/hooks/results/useImpressionLogger';
export { usePreviewPlayback } from '@features/discovery/hooks/results/usePreviewPlayback';
export { useResultsFilter } from '@features/discovery/view/results/useResultsFilter';
export { useResultTap } from '@features/discovery/hooks/results/useResultTap';
export { useRefreshFromFirstPage } from '@features/discovery/view/search/useSearchRefresh';
