import { transientRetryOptions } from '@shared/query';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/searchHistory';

export function useSettingsClearSearchHistory() {
  return useSharedClearSearchHistory({
    action: 'settings.clear_search_history',
    retryOptions: transientRetryOptions,
  });
}
