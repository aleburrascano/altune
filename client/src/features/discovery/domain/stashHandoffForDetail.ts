import { detailHref, type DetailHref } from '@shared/navigation';
import type { DiscoveryResult } from '@shared/catalog';

export function stashHandoffForDetail(
  result: DiscoveryResult,
  searchId?: string,
): DetailHref<'/discover/detail'> {
  return detailHref('/discover/detail', result, searchId);
}
