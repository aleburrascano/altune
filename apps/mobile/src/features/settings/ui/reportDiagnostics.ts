import Constants from 'expo-constants';
import { Platform } from 'react-native';

export type ReportDiagnostics = {
  app_version: string;
  platform: string;
  os_version: string;
  screen: string;
};

export function reportDiagnostics(screen: string): ReportDiagnostics {
  return {
    app_version: Constants.expoConfig?.version ?? 'dev',
    platform: Platform.OS,
    os_version: String(Platform.Version),
    screen,
  };
}

export function diagnosticsSummary(diagnostics: ReportDiagnostics): string {
  return `Altune ${diagnostics.app_version} · ${diagnostics.platform} ${diagnostics.os_version} · ${diagnostics.screen}`;
}
