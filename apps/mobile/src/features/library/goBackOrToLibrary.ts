import type { useRouter } from 'expo-router';

export function goBackOrToLibrary(router: ReturnType<typeof useRouter>): void {
  if (router.canGoBack()) {
    router.back();
    return;
  }
  router.replace('/library');
}
