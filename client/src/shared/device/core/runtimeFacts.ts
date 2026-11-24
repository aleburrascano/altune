import type { UpdatesState } from '@shared/device/ports/appRuntime';

const EXPO_GO_EXECUTION_ENVIRONMENT = 'storeClient';

export function runningUpdateIdOf(updates: UpdatesState): string | null {
  if (!updates.isEnabled || updates.isEmbeddedLaunch) return null;
  return updates.updateId;
}

export function isExpoGoEnvironment(executionEnvironment: string): boolean {
  return executionEnvironment === EXPO_GO_EXECUTION_ENVIRONMENT;
}
