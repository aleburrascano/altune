import { asyncView } from '@shared/lib/async-view';

export type ScreenView = 'loading' | 'error' | 'empty' | 'list';

export function _viewForState(state: {
  isLoading: boolean;
  error: Error | null;
  items: readonly unknown[];
}): ScreenView {
  const view = asyncView({
    isLoading: state.isLoading,
    isError: Boolean(state.error),
    isEmpty: state.items.length === 0,
  });
  return view === 'ready' ? 'list' : view;
}
