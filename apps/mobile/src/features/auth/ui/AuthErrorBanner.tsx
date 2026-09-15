import type { ReactElement } from 'react';

import { Banner } from '@shared/ui/primitives/Banner';

import { authErrorText, type AuthErrorReason } from '../errorCopy';

type AuthErrorBannerState =
  | { kind: 'error'; reason: AuthErrorReason }
  | { kind: 'idle' | 'pending' | 'sent' | 'ok' };

/**
 * Renders the shared `auth-error` banner for an async auth action, or nothing
 * when the action is not in its error state. Wraps `authErrorText` so every
 * caller renders identical markup, tone, and `testID`.
 */
export function AuthErrorBanner({
  state,
  generic,
}: {
  state: AuthErrorBannerState;
  generic: string;
}): ReactElement | null {
  if (state.kind !== 'error') return null;
  return (
    <Banner testID="auth-error" tone="danger">
      {authErrorText(state.reason, generic)}
    </Banner>
  );
}
