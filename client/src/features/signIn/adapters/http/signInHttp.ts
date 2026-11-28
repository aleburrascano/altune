import { apiBase } from '@shared/apiConfig';
import { appVersion } from '@shared/device';
import { warn } from '@shared/log';

import { toFailureReportWire } from '@features/signIn/adapters/http/mapping';
import { thrownErrorDetail, type SignInFailureReason } from '@features/signIn/core/errors';
import type { AuthPort } from '@features/signIn/ports/ports';

const REPORT_TIMEOUT_MS = 5_000;

function postFailure(reason: SignInFailureReason, signal: AbortSignal): Promise<unknown> {
  return fetch(`${apiBase()}/v1/public/auth-failures`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(toFailureReportWire(reason, appVersion())),
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

function reportSignInFailure(reason: SignInFailureReason): void {
  postWithTimeout(reason).catch((error: unknown) => {
    warn('[auth] sign-in failure report not delivered', {
      reason,
      ...thrownErrorDetail(error),
    });
  });
}

export const httpAuthPort: AuthPort = {
  reportSignInFailure: (reason) => reportSignInFailure(reason),
};
