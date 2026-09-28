import { type ReactElement, type ReactNode } from 'react';

import type { AsyncView } from '@shared/lib/async-view';

export interface AsyncSectionProps {
  view: AsyncView;
  skeleton: () => ReactNode;
  error: () => ReactNode;
  empty: () => ReactNode;
  children: ReactNode;
}

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
