import Constants from 'expo-constants';
import { Platform } from 'react-native';

import type { SubmitReportInput } from '@shared/api-client/feedback';

export type ReportDiagnostics = Omit<SubmitReportInput, 'kind' | 'message'>;

export function reportDiagnostics(screen: string): ReportDiagnostics {
  return {
    app_version: Constants.expoConfig?.version ?? 'dev',
    platform: Platform.OS,
    os_version: String(Platform.Version),
    screen,
  };
}
