import { Platform } from 'react-native';

import { useLayoutMode } from '@shared/ui/layout/useLayoutMode';

export function useWideWebLayout(): boolean {
  const layoutMode = useLayoutMode();
  return Platform.OS === 'web' && layoutMode === 'wide';
}
