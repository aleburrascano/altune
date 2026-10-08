import type { FeaturedArtist } from '@shared/api-client/types';
import { useNavigator } from '@shared/navigation';

import { featuringHref, type DetailRoute } from '@/features/detail/domain/detailNavigation';

export function useOpenFeaturing(detailRoute: DetailRoute): (artist: FeaturedArtist) => void {
  const navigator = useNavigator();
  return (artist) => navigator.push(featuringHref(detailRoute, artist));
}
