import { useWindowDimensions } from 'react-native';

import { isWebPlatform } from '@shared/device/isWebPlatform';
import { WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/layout/breakpoints';

export type LayoutMode = 'compact' | 'wide';

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
