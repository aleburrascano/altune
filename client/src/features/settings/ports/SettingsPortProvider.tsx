import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { SettingsPort } from '@features/settings/ports/settingsPort';

const SettingsPortContext = createContext<SettingsPort | null>(null);

interface SettingsPortProviderProps {
  port: SettingsPort;
  children: ReactNode;
}

export function SettingsPortProvider({ port, children }: SettingsPortProviderProps): ReactElement {
  return <SettingsPortContext.Provider value={port}>{children}</SettingsPortContext.Provider>;
}

export function useSettingsPort(): SettingsPort {
  const port = use(SettingsPortContext);
  if (port === null) throw new Error('no settings port provided: mount SettingsPortProvider');
  return port;
}
