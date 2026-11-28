import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';

import type { SettingsPort } from '@features/settings/ports/ports';

const SettingsPortContext = createContext<SettingsPort | null>(null);

interface SettingsProviderProps {
  port: SettingsPort;
  children: ReactNode;
}

export function SettingsProvider({ port, children }: SettingsProviderProps): ReactElement {
  return <SettingsPortContext.Provider value={port}>{children}</SettingsPortContext.Provider>;
}

export function useSettingsPort(): SettingsPort {
  const port = use(SettingsPortContext);
  if (port === null) throw new Error('no settings port provided: mount SettingsProvider');
  return port;
}
