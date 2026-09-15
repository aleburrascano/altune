import { createContext, useContext } from 'react';

import type { DetailHandoff } from '@shared/lib/detail-handoff';

// DetailScreen resolves its own handoff from the route param and provides it,
// so the save / play / wrong-album hooks several components down attribute
// events to the search that opened *this* screen, without threading the
// handoff through every body component and state hook.
const DetailHandoffContext = createContext<DetailHandoff | null>(null);

export const DetailHandoffProvider = DetailHandoffContext.Provider;

export function useDetailHandoff(): DetailHandoff | null {
  return useContext(DetailHandoffContext);
}
