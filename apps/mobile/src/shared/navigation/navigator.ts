import type { Href } from './expoRouterNavigator';

export interface Navigator {
  push(href: Href): void;
  replace(href: Href): void;
  back(): void;
  canGoBack(): boolean;
}

export type TabRoot = 'discover' | 'library';

export function tabRootFromSegments(segments: readonly string[]): TabRoot {
  return segments[1] === 'library' ? 'library' : 'discover';
}
