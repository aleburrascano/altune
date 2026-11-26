import type { RetryOptions } from '@features/settings/view/retryOptions';
import { useClearSearchHistory as useSharedClearSearchHistory } from '@shared/searchHistory';

export function useSettingsClearSearchHistory(retryOptions: RetryOptions) {
  return useSharedClearSearchHistory({
    action: 'settings.clear_search_history',
    retryOptions,
  });
}
