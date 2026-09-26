import type { ReactNode } from 'react';
import { useRouter, useSegments, usePathname } from 'expo-router';

import { TAB_ROUTES, type TabRoute } from '@shared/ui/navigation/tabRoutes';

import { WideChrome } from './wideChrome';

function activeTabRouteFor(pathname: string): TabRoute {
  const match = TAB_ROUTES.find((route) => pathname.startsWith(`/${route}`));
  return match ?? 'discover';
}

function useShowWideChrome(isWideWeb: boolean): boolean {
  const segments = useSegments();
  const first = segments[0] as string | undefined;
  const isChromeRoute = first === '(tabs)' || first === 'player';
  return isWideWeb && isChromeRoute;
}

export type AppChromeProps = { isWideWeb: boolean; children: ReactNode };

function navigateToTab(router: ReturnType<typeof useRouter>) {
  return (route: TabRoute) => router.push(`/${route}`);
}

export function AppChrome({ isWideWeb, children }: AppChromeProps) {
  const pathname = usePathname();
  const router = useRouter();
  if (!useShowWideChrome(isWideWeb)) return <>{children}</>;

  return (
    <WideChrome activeRoute={activeTabRouteFor(pathname)} onNavigate={navigateToTab(router)}>
      {children}
    </WideChrome>
  );
}
