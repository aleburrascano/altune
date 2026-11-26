import type { ReactElement } from 'react';

import { useNavigator } from '@shared/navigation';
import { LibraryScreen } from '@features/library';

export function LibraryRoute(): ReactElement {
  return <LibraryScreen navigator={useNavigator()} />;
}
