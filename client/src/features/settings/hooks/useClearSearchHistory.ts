import { transientRetryOptions } from '@shared/query/retryDelay';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/search-history/useClearSearchHistory';

export function useSettingsClearSearchHistory() {
  return useSharedClearSearchHistory({
    action: 'settings.clear_search_history',
    retryOptions: transientRetryOptions,
  });
}
