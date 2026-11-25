import type { DiscoveryResult } from '@shared/catalog';
import { detailHref, readDetailHandoff, type DetailHandoff } from '@shared/navigation';

export function handoffFor(
  result: DiscoveryResult,
  route: '/discover/detail' | '/library/detail' | '/discover/unreleased' = '/discover/detail',
  searchId?: string,
): DetailHandoff {
  const handoff = readDetailHandoff(detailHref(route, result, searchId).params.handoff);
  if (handoff === null) throw new Error('the handoff just stored was not found');
  return handoff;
}
