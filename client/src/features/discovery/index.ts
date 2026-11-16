export { DiscoverScreen } from '@features/discovery/ui/search/DiscoverScreen';
export { registerSearchStateSignOutReset } from '@features/discovery/adapters/registerSearchStateSignOutReset';
export {
  DiscoverPortProvider,
  useDiscoverPort,
} from '@features/discovery/view/DiscoverPortProvider';
export {
  FavoritesPortProvider,
  useFavoritesPort,
} from '@features/discovery/view/FavoritesPortProvider';
export { UnreleasedHitsSlotProvider } from '@features/discovery/view/UnreleasedHitsSlot';
export { httpDiscoverPort } from '@features/discovery/adapters/http/httpDiscoverPort';
export { httpFavoritesPort } from '@features/discovery/adapters/http/httpFavoritesPort';
export type { DiscoverPort } from '@features/discovery/ports/discoverPort';
export type { FavoritesPort } from '@features/discovery/ports/favoritesPort';
export { BlendedSection } from '@features/discovery/view/results/BlendedSection';
export { resultFixture } from '@features/discovery/__tests__/fixtures';
export type { ResultsCommonProps } from '@features/discovery/view/results/ResultsList';
export { buildImpressionRows } from '@features/discovery/core/buildImpressionRows';
export { DiscoverBody } from '@features/discovery/view/search/DiscoverBody';
export type {
  DiscoverHookState,
  DiscoverView,
  SearchCorrection,
} from '@features/discovery/core/discoverView';
export { createMemoryDiscoverPort } from '@features/discovery/__tests__/memoryDiscoverPort';
export {
  useAutocompleteSuggestions,
  useSuggestionVisibility,
} from '@features/discovery/view/history/useSuggestions';
export { useDiscoverSearch } from '@features/discovery/view/search/useDiscoverSearch';
export {
  useDiscoverClearSearchHistory,
  useSearchHistory,
} from '@features/discovery/view/history/useSearchHistory';
export { CorrectionBanner } from '@features/discovery/view/results/CorrectionBanner';
export { DiscoverRow } from '@features/discovery/view/results/DiscoverRow';
export {
  asyncViewForDiscoverView,
  correctionForResponse,
  resultsIncompleteForState,
  searchAnnouncement,
  viewForState,
} from '@features/discovery/core/discoverView';
export { createMemoryFavoritesPort } from '@features/discovery/__tests__/memoryFavoritesPort';
export { FilterChips } from '@features/discovery/view/results/FilterChips';
export { FilteredResults } from '@features/discovery/view/results/FilteredResults';
export { kindLabel, resultKey, resultSecondaryLine } from '@features/discovery/core/resultDisplay';
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
} from '@features/discovery/core/searchPaging';
export {
  getSearchState,
  resetSearchState,
  setSearchState,
} from '@features/discovery/core/searchState';
export { SectionGrid } from '@features/discovery/view/results/SectionGrid';
export { TopResultCard } from '@features/discovery/view/results/TopResultCard';
export { stashHandoffForDetail } from '@features/discovery/domain/stashHandoffForDetail';
export { useDebouncedSearch } from '@features/discovery/view/search/useDebouncedSearch';
export { useDiscoverLogic } from '@features/discovery/view/search/useDiscoverLogic';
export { useFavorites } from '@features/discovery/view/favorites/useFavorites';
export { useImpressionLogger } from '@features/discovery/view/results/useImpressionLogger';
export { usePreviewPlayback } from '@features/discovery/view/results/usePreviewPlayback';
export { useResultsFilter } from '@features/discovery/view/results/useResultsFilter';
export { useResultTap } from '@features/discovery/hooks/results/useResultTap';
export { useRefreshFromFirstPage } from '@features/discovery/view/search/useSearchRefresh';
