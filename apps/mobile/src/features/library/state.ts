/**
 * Pure state-machine helpers for the library feature.
 *
 * Lives in its own module (no React Native imports) so unit tests under
 * `__tests__/` can import these without jest needing RN transforms.
 */

import type { LibraryState } from './hooks/useLibrary';

export type ScreenView = 'loading' | 'error' | 'empty' | 'list';

/**
 * Derives which sub-view the LibraryScreen should render from the hook's
 * state. Order matters: loading > error > empty > list. Reasoning:
 *   - loading first because mid-load the items array is empty by definition
 *     (or stale from a prior load), so "empty" would mis-fire.
 *   - error over empty because a fetch failure is a real surface, not "no
 *     data" — AC#6 requires the retry path.
 */
export function _viewForState(
  state: Pick<LibraryState, 'isLoading' | 'error' | 'items'>,
): ScreenView {
  if (state.isLoading) {
    return 'loading';
  }
  if (state.error) {
    return 'error';
  }
  if (state.items.length === 0) {
    return 'empty';
  }
  return 'list';
}
