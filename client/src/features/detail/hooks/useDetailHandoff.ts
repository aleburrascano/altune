import { createContext, use } from 'react';

import type { DetailHandoff } from '@shared/navigation/detailHandoff';

const DetailHandoffContext = createContext<DetailHandoff | null>(null);

export const DetailHandoffProvider = DetailHandoffContext.Provider;

export function useDetailHandoff(): DetailHandoff | null {
  return use(DetailHandoffContext);
}
