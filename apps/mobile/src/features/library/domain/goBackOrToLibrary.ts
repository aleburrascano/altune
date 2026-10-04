import type { Navigator } from '@shared/navigation';

export function goBackOrToLibrary(navigator: Navigator): void {
  if (navigator.canGoBack()) {
    navigator.back();
    return;
  }
  navigator.replace('/library');
}
