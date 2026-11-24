import { isWebPlatform } from '@shared/device/core/isWebPlatform';
import { webOrigin } from '@shared/device/core/webOrigin';

export { isWebPlatform, webOrigin };
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
