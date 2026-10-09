import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import { httpSettingsPort } from '@features/settings/adapters/http/httpSettingsPort';
import type { SettingsPort } from '@features/settings/ports/settingsPort';

const SettingsPortContext = createContext<SettingsPort>(httpSettingsPort);

interface SettingsPortProviderProps {
  port: SettingsPort;
  children: ReactNode;
}

export function SettingsPortProvider({ port, children }: SettingsPortProviderProps): ReactElement {
  return <SettingsPortContext.Provider value={port}>{children}</SettingsPortContext.Provider>;
}

export function useSettingsPort(): SettingsPort {
  return use(SettingsPortContext);
}
