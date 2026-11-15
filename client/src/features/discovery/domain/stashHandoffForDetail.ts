import { detailHref, type DetailHref } from '@shared/navigation';

import type { DiscoveryResult } from '@shared/apiClient';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<'/discover/detail'> {
  return detailHref('/discover/detail', result, searchId);
}
