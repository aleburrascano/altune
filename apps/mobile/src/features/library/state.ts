/**
 * Pure state-machine helpers for the library feature.
 *
 * Lives in its own module (no React Native imports) so unit tests under
 * `__tests__/` can import these without jest needing RN transforms.
 */

import { asyncView } from '@shared/lib/async-view';

import type { LibraryState } from './hooks/useLibrary';

export type ScreenView = 'loading' | 'error' | 'empty' | 'list';

/**
 * Derives which sub-view the LibraryScreen should render from the hook's
 * state. The loading > error > empty > ready precedence lives in the shared
 * async-view spine; this maps `ready` onto the library's `list` vocabulary.
 *   - loading first because mid-load the items array is empty by definition
 *     (or stale from a prior load), so "empty" would mis-fire.
 *   - error over empty because a fetch failure is a real surface, not "no
 *     data" — AC#6 requires the retry path.
 */
export function _viewForState(
  state: Pick<LibraryState, 'isLoading' | 'error' | 'items'>,
): ScreenView {
  const view = asyncView({
    isLoading: state.isLoading,
    isError: Boolean(state.error),
    isEmpty: state.items.length === 0,
  });
  return view === 'ready' ? 'list' : view;
}
