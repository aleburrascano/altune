import { resolveApiBase } from '@shared/apiConfig/core/config';

const BASE_BEFORE_THE_APP_INSTALLS_ONE = resolveApiBase(undefined, true);

let installed: string | undefined;

export function installApiBase(value: string | undefined, isDev: boolean): void {
  installed = resolveApiBase(value, isDev);
}

export function apiBase(): string {
  return installed ?? BASE_BEFORE_THE_APP_INSTALLS_ONE;
}
