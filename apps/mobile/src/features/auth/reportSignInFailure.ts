import { apiBase } from '@shared/api-client';
import { appVersion } from '@shared/device/device';
import { isLoopEnabled } from '@shared/killSwitch/killSwitch';

import type { SignInFailureReason } from './errorReason';

const REPORT_TIMEOUT_MS = 5_000;

function postFailure(reason: SignInFailureReason, signal: AbortSignal): Promise<unknown> {
  return fetch(`${apiBase}/v1/public/auth-failures`, {
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
  if (!isLoopEnabled('telemetryFlush')) return;
  postWithTimeout(reason).catch(() => undefined);
}
