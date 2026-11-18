import { createContext, use } from 'react';
import type { ReactElement, ReactNode } from 'react';
import type { Navigator } from '@shared/navigation';

const DetailNavigatorContext = createContext<Navigator | null>(null);

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

export function useProvidedNavigator(): Navigator | null {
  return use(DetailNavigatorContext);
}
