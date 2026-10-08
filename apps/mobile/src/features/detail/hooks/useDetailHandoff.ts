import { createContext, useContext } from 'react';

import type { DetailHandoff } from '@shared/lib/detail-handoff';

const DetailHandoffContext = createContext<DetailHandoff | null>(null);

export const DetailHandoffProvider = DetailHandoffContext.Provider;

export function useDetailHandoff(): DetailHandoff | null {
  return useContext(DetailHandoffContext);
}
