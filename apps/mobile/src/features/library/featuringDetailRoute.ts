import { tabRootFromSegments } from '@shared/navigation/navigator';

export type FeaturingDetailRoute = '/library/detail' | '/discover/detail';

export function featuringDetailRoute(segments: readonly string[]): FeaturingDetailRoute {
  return `/${tabRootFromSegments(segments)}/detail`;
}
