import Constants from 'expo-constants';
import { Platform } from 'react-native';

export type DevicePlatform = 'ios' | 'android' | 'web' | 'windows' | 'macos';

export interface DeviceInfo {
  platform: DevicePlatform;
  osVersion: string;
  appVersion: string;
}

export function appVersion(): string {
  return Constants.expoConfig?.version ?? 'dev';
}

export function deviceInfo(): DeviceInfo {
  return {
    platform: Platform.OS,
    osVersion: String(Platform.Version),
    appVersion: appVersion(),
  };
}

export function declaredAppScheme(): string | string[] | undefined {
  return Constants.expoConfig?.scheme;
}

export function webOrigin(): string | null {
  return Platform.OS === 'web' && typeof window !== 'undefined' ? window.location.origin : null;
}
