import type { SubmitReportInput } from '@shared/api-client/feedback';
import { deviceInfo } from '@shared/device/device';

export type ReportDiagnostics = Omit<SubmitReportInput, 'kind' | 'message'>;

export function reportDiagnostics(screen: string): ReportDiagnostics {
  const { appVersion, platform, osVersion } = deviceInfo();
  return {
    app_version: appVersion,
    platform,
    os_version: osVersion,
    screen,
  };
}
