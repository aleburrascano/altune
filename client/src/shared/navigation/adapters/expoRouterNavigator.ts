import { useFocusEffect, useRouter, useSegments } from 'expo-router';

export type { Href } from 'expo-router';

export function useExpoRouter(): ReturnType<typeof useRouter> {
  return useRouter();
}

export function useRouteSegments(): string[] {
  return useSegments();
}

export function useScreenFocusEffect(effect: () => void | (() => void)): void {
  useFocusEffect(effect);
}
