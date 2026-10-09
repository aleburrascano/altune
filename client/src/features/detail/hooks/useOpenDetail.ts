import type { DiscoveryResult } from '@shared/api-client/discovery';
import { useNavigator } from '@shared/navigation';

import { openDetail, type DetailRoute } from '@/features/detail/domain/detailNavigation';

export function useOpenDetail(detailRoute: DetailRoute): (picked: DiscoveryResult) => void {
  const navigator = useNavigator();
  return (picked) => openDetail(navigator, detailRoute, picked);
}
