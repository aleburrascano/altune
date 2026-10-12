import { useWindowDimensions } from 'react-native';

import { isWebPlatform } from '@shared/device/isWebPlatform';

export type LayoutMode = 'compact' | 'wide';

export const WIDE_LAYOUT_MIN_WIDTH = 1000;
export const CONTENT_MAX_WIDTH = 1200;

export function layoutModeFor(width: number): LayoutMode {
  return width >= WIDE_LAYOUT_MIN_WIDTH ? 'wide' : 'compact';
}

export function useLayoutMode(): LayoutMode {
  const { width } = useWindowDimensions();
  return layoutModeFor(width);
}

export function useWideWebLayout(): boolean {
  const layoutMode = useLayoutMode();
  return layoutMode === 'wide' && isWebPlatform();
}
