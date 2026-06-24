/**
 * async-view — the shared precedence spine for a screen's async sub-view.
 *
 * Loading > error > empty > ready. This order is the one fact both the library
 * and discover screens were re-deriving independently; it now lives here once.
 * Features keep their own `_viewForState` wrappers that decide what counts as
 * loading/error/empty (e.g. discover gates loading on "no data yet") and map
 * the `ready` verdict onto their feature-specific vocabulary (`list`,
 * `results`, …). Promote-on-second-consumer satisfied: library + discover.
 */

export type AsyncView = 'loading' | 'error' | 'empty' | 'ready';

export type AsyncInputs = {
  isLoading: boolean;
  isError: boolean;
  isEmpty: boolean;
};

export function asyncView({ isLoading, isError, isEmpty }: AsyncInputs): AsyncView {
  if (isLoading) {
    return 'loading';
  }
  if (isError) {
    return 'error';
  }
  if (isEmpty) {
    return 'empty';
  }
  return 'ready';
}
