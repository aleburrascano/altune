import Constants, { ExecutionEnvironment } from 'expo-constants';
import { Platform } from 'react-native';
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

const BUILD_COMMIT_LENGTH = 12;

export function platformName(): string {
  return Platform.OS;
}

function buildCommit(): string {
  return process.env.EXPO_PUBLIC_BUILD_COMMIT?.slice(0, BUILD_COMMIT_LENGTH) || 'dev';
}

export function telemetryVersion(): string {
  return `${buildCommit()}/${appVersionLabel()}`;
}

export function declaredAppScheme(): string | string[] | undefined {
  return Constants.expoConfig?.scheme;
}

export function isExpoGoRuntime(): boolean {
  return Constants.executionEnvironment === ExecutionEnvironment.StoreClient;
}

export { isWebPlatform } from '@shared/device/isWebPlatform';
export { webOrigin } from '@shared/device/webOrigin';
