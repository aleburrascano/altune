import type { ReactElement } from 'react';

import type { SortKey } from './sort';

export type LibraryChip = 'playlists' | 'tracks' | 'albums' | 'artists';

export type ActiveView = {
  content: ReactElement;
  count: number;
  noun: string;
  options: { key: SortKey; label: string }[];
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
};
