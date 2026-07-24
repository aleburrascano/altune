import { setDetailHandoff } from '@shared/lib/detail-handoff';

import type { DiscoveryResult } from '@shared/api-client/discovery';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): '/discover/detail' {
  setDetailHandoff(result, searchId);
  return '/discover/detail';
}
