import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';

import { authErrorText, type AuthErrorReason } from '../errorCopy';

type AuthErrorBannerState =
  | { kind: 'error'; reason: AuthErrorReason }
  | { kind: 'idle' | 'pending' | 'sent' | 'ok' | 'cancelled' };

/**
 * Renders the shared error banner for an async auth action, or nothing when the
 * action is not in its error state. Wraps `authErrorText` so every caller
 * renders identical markup and tone. `testID` is only overridden where two
 * actions can show a banner in one screen at once (the form and OAuth).
 */
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
    <Banner testID={testID} tone="danger">
      {authErrorText(state.reason, generic)}
    </Banner>
  );
}
