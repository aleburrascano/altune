import { detailHref, type DetailHref } from '@shared/navigation/detailHandoff';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<'/discover/detail'> {
  return detailHref('/discover/detail', result, searchId);
}
