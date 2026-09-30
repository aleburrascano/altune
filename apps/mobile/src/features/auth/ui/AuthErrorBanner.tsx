import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';

import { authErrorText, type AuthErrorReason } from '../errorReason';

type AuthErrorBannerState =
  | { kind: 'error'; reason: AuthErrorReason }
  | { kind: 'idle' | 'pending' | 'sent' | 'ok' | 'cancelled' };

export function AuthErrorBanner({
  state,
  generic,
  testID = 'auth-error',
}: {
  state: AuthErrorBannerState;
  generic: string;
  testID?: string;
}): ReactElement | null {
  if (state.kind !== 'error') return null;
  return (
    <Banner testID={testID} tone="danger" surface="auth.error">
      {authErrorText(state.reason, generic)}
    </Banner>
  );
}
