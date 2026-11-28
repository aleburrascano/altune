import type { SignInFailureReason } from '@features/signIn/core/errors';

export type SignInFailureReportWire = { reason: SignInFailureReason; app_version: string };

export function toFailureReportWire(
  reason: SignInFailureReason,
  appVersion: string,
): SignInFailureReportWire {
  return { reason, app_version: appVersion };
}
