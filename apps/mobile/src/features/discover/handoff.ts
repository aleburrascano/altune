import { detailHref, type DetailHref } from '@shared/lib/detail-handoff';

import type { DiscoveryResult } from '@shared/api-client/discovery';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<'/discover/detail'> {
  return detailHref('/discover/detail', result, searchId);
}
