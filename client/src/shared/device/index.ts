import { createExpoAppRuntime } from '@shared/device/adapters/expoAppRuntime';
import { isWebPlatform } from '@shared/device/core/isWebPlatform';
import { webOrigin } from '@shared/device/core/webOrigin';

export { createExpoAppRuntime, isWebPlatform, webOrigin };
export type { ExpoSources } from '@shared/device/adapters/expoAppRuntime';
export type { AppRuntime, Build, UpdatesState } from '@shared/device/ports/appRuntime';
export {
  appVersion,
  appVersionLabel,
  declaredAppScheme,
  installAppRuntime,
  installBuild,
  isExpoGoRuntime,
  platformName,
  telemetryVersion,
} from '@shared/device/shell/device';
