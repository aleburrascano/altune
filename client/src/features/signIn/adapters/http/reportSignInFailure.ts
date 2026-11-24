import { apiBase } from '@shared/apiConfig';
import { appVersion } from '@shared/device';
import { warn } from '@shared/log';

import { thrownErrorDetail, type SignInFailureReason } from '@features/signIn/core/authError';

const REPORT_TIMEOUT_MS = 5_000;

function postFailure(reason: SignInFailureReason, signal: AbortSignal): Promise<unknown> {
  return fetch(`${apiBase()}/v1/public/auth-failures`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ reason, app_version: appVersion() }),
    signal,
  });
}

async function postWithTimeout(reason: SignInFailureReason): Promise<void> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REPORT_TIMEOUT_MS);
  try {
    await postFailure(reason, controller.signal);
  } finally {
    clearTimeout(timer);
  }
}

export function reportSignInFailure(reason: SignInFailureReason): void {
  postWithTimeout(reason).catch((error: unknown) => {
    warn('[auth] sign-in failure report not delivered', {
      reason,
      ...thrownErrorDetail(error),
    });
  });
}
