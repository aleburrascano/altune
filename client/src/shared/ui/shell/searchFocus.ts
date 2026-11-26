import type { SearchFocus } from '@shared/ui/ports/searchFocus';

const DISCOVER_ROUTE = '/discover';
const FOCUS_REQUEST_TTL_MS = 5_000;

export function createSearchFocus(): SearchFocus {
  let registeredFocus: (() => void) | null = null;
  let pendingFocusTimer: ReturnType<typeof setTimeout> | null = null;

  function clearPendingFocusRequest(): void {
    if (pendingFocusTimer === null) return;
    clearTimeout(pendingFocusTimer);
    pendingFocusTimer = null;
  }

  function register(focus: () => void): () => void {
    registeredFocus = focus;
    if (pendingFocusTimer !== null) {
      clearPendingFocusRequest();
      focus();
    }
    return () => {
      if (registeredFocus === focus) registeredFocus = null;
    };
  }

  function requestFocus(navigator: Parameters<SearchFocus['requestFocus']>[0]): void {
    if (registeredFocus) {
      registeredFocus();
      return;
    }
    if (pendingFocusTimer !== null) return;
    pendingFocusTimer = setTimeout(clearPendingFocusRequest, FOCUS_REQUEST_TTL_MS);
    try {
      navigator.push(DISCOVER_ROUTE);
    } catch {
      clearPendingFocusRequest();
    }
  }

  return { register, requestFocus };
}
