import { createContext, createElement, use } from 'react';
import type { ComponentType, ReactElement, ReactNode } from 'react';

import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';

export interface UnreleasedHitsViewProps {
  hits: UnreleasedSearchHit[];
  limit?: number;
}

type UnreleasedHitsView = ComponentType<UnreleasedHitsViewProps>;

function RendersNothing(): null {
  return null;
}

const UnreleasedHitsContext = createContext<UnreleasedHitsView>(RendersNothing);

interface UnreleasedHitsSlotProviderProps {
  view: UnreleasedHitsView;
  children: ReactNode;
}

export function UnreleasedHitsSlotProvider({
  view,
  children,
}: UnreleasedHitsSlotProviderProps): ReactElement {
  return <UnreleasedHitsContext.Provider value={view}>{children}</UnreleasedHitsContext.Provider>;
}

export function UnreleasedHitsSlot(props: UnreleasedHitsViewProps): ReactElement {
  return createElement(use(UnreleasedHitsContext), props);
}
