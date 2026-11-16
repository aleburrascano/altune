import { useFocusEffect, useRouter, useSegments } from 'expo-router';

import type { Navigator } from '@shared/navigation/shell/navigator';

export type { Href } from 'expo-router';

export function useNavigator(): Navigator {
  return useRouter();
}

export function useRouteSegments(): string[] {
  return useSegments();
}

export function useScreenFocusEffect(effect: () => void | (() => void)): void {
  useFocusEffect(effect);
}
