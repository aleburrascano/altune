import { tabRootFromSegments } from '@shared/navigation/navigator';
import type { Navigator } from '@shared/navigation';

export type FeaturingDetailRoute = '/library/detail' | '/discover/detail';

export function featuringDetailRoute(segments: readonly string[]): FeaturingDetailRoute {
  return `/${tabRootFromSegments(segments)}/detail`;
}

export function goBackOrToLibrary(navigator: Navigator): void {
  if (navigator.canGoBack()) {
    navigator.back();
    return;
  }
  navigator.replace('/library');
}
