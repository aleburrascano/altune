import { expoAppRuntime } from '@shared/device/adapters/expoAppRuntime';
import { isExpoGoEnvironment, runningUpdateIdOf } from '@shared/device/core/runtimeFacts';
import { telemetryVersionOf, versionLabel, versionOf } from '@shared/device/core/versionLabel';
import type { AppRuntime, Build } from '@shared/device/ports/appRuntime';

let runtime: AppRuntime = expoAppRuntime;

const BUILD_BEFORE_THE_APP_INSTALLS_ONE: Build = { platform: 'unknown', commit: undefined };

let build: Build = BUILD_BEFORE_THE_APP_INSTALLS_ONE;

export function installAppRuntime(next: AppRuntime = expoAppRuntime): void {
  runtime = next;
}

export function installBuild(next: Build): void {
  build = next;
}

export function appVersion(): string {
  return versionOf(runtime.manifestVersion());
}

export function appVersionLabel(): string {
  return versionLabel(appVersion(), runningUpdateIdOf(runtime.updates()));
}

export function platformName(): string {
  return build.platform;
}

export function telemetryVersion(): string {
  return telemetryVersionOf(build.commit, appVersionLabel());
}

export function declaredAppScheme(): string | string[] | undefined {
  return runtime.declaredScheme();
}

export function isExpoGoRuntime(): boolean {
  return isExpoGoEnvironment(runtime.executionEnvironment());
}
