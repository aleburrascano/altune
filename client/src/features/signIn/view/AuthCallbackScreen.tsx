import { type ReactElement } from 'react';

import { AppRedirect } from '@shared/navigation';

export function AuthCallbackScreen(): ReactElement {
  return <AppRedirect href="/" />;
}
