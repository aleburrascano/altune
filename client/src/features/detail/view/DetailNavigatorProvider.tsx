import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';
import type { Navigator } from '@shared/navigation';

const DetailNavigatorContext = createContext<Navigator | null>(null);

const MISSING_NAVIGATOR = 'Detail views must be rendered inside a DetailNavigatorProvider';

interface DetailNavigatorProviderProps {
  navigator: Navigator;
  children: ReactNode;
}

export function DetailNavigatorProvider({
  navigator,
  children,
}: DetailNavigatorProviderProps): ReactElement {
  return (
    <DetailNavigatorContext.Provider value={navigator}>{children}</DetailNavigatorContext.Provider>
  );
}

export function useProvidedNavigator(): Navigator {
  const navigator = use(DetailNavigatorContext);
  if (navigator === null) throw new Error(MISSING_NAVIGATOR);
  return navigator;
}
