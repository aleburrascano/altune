import type { ReactElement } from 'react';

import type { SortKey } from './sort';

// The contract every chip's view hook satisfies, so the library screen renders one
// shape whichever chip is selected.
export type ActiveView = {
  content: ReactElement;
  count: number;
  noun: string;
  options: { key: SortKey; label: string }[];
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
};
