import type { ReactElement } from 'react';

import { transientRetryOptions } from '@shared/query';
import { SettingsScreen } from '@features/settings';

export function SettingsRoute(): ReactElement {
  return <SettingsScreen retryOptions={transientRetryOptions} />;
}
