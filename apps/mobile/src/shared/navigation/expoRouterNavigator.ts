import { useFocusEffect, useRouter, useSegments } from 'expo-router';

import type { Navigator } from './navigator';

export type { Href } from 'expo-router';

export function useNavigator(): Navigator {
  return useRouter();
}

export function useCurrentSegments(): readonly string[] {
  return useSegments();
}

export function useScreenFocusEffect(effect: () => void | (() => void)): void {
  useFocusEffect(effect);
}
