import { type ReactElement, type ReactNode } from 'react';

import type { AsyncView } from '@shared/lib/async-view';

export interface AsyncSectionProps {
  /** The computed async state — the visual twin of {@link asyncView}. */
  view: AsyncView;
  /** Builds the loading placeholder. Called only while loading. */
  skeleton: () => ReactNode;
  /** Builds the error state (message + retry). Called only on error. */
  error: () => ReactNode;
  /** Builds the empty state. Called only when there is nothing to show. */
  empty: () => ReactNode;
  /** Rendered when content is ready. */
  children: ReactNode;
}

/**
 * Presenter twin of {@link asyncView}: takes the already-computed async state
 * and renders the matching slot in a fixed loading → error → empty → ready
 * precedence. Slots are builder functions so only the active state's JSX is
 * constructed, and the per-state JSX (skeleton, "couldn't load" + retry, empty
 * copy) lives in each call site — drifted copy and retry affordances are
 * preserved verbatim while the branch order stops being copy-pasted.
 */
export function AsyncSection({
  view,
  skeleton,
  error,
  empty,
  children,
}: AsyncSectionProps): ReactElement {
  if (view === 'loading') return <>{skeleton()}</>;
  if (view === 'error') return <>{error()}</>;
  if (view === 'empty') return <>{empty()}</>;
  return <>{children}</>;
}
