export type FeaturingDetailRoute = '/library/detail' | '/discover/detail';

export function featuringDetailRoute(segments: readonly string[]): FeaturingDetailRoute {
  return segments[1] === 'discover' ? '/discover/detail' : '/library/detail';
}
