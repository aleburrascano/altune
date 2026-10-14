import Constants, { ExecutionEnvironment } from 'expo-constants';
import * as Updates from 'expo-updates';

export function appVersion(): string {
  return Constants.expoConfig?.version ?? 'dev';
}

const SHORT_UPDATE_ID_LENGTH = 8;

function runningUpdateId(): string | null {
  if (!Updates.isEnabled || Updates.isEmbeddedLaunch) return null;
  return Updates.updateId;
}

export function appVersionLabel(): string {
  const updateId = runningUpdateId();
  if (updateId == null) return appVersion();
  return `${appVersion()} · update ${updateId.slice(0, SHORT_UPDATE_ID_LENGTH)}`;
}

export function declaredAppScheme(): string | string[] | undefined {
  return Constants.expoConfig?.scheme;
}

export function isExpoGoRuntime(): boolean {
  return Constants.executionEnvironment === ExecutionEnvironment.StoreClient;
}

export { isWebPlatform } from '@shared/device/isWebPlatform';
export { webOrigin } from '@shared/device/webOrigin';
