import type { useRouter } from 'expo-router';

/**
 * Leaving a library screen. A deep link can open one with no history behind it, and a
 * delete can remove the screen the user came from, so back is not always available:
 * the library root is where the module falls back to.
 */
export function goBackOrToLibrary(router: ReturnType<typeof useRouter>): void {
  if (router.canGoBack()) {
    router.back();
    return;
  }
  router.replace('/library');
}
