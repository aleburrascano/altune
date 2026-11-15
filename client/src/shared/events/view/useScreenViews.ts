import { useEffect, useRef } from 'react';

type ScreenViewSender = (route: string) => void;

export function useScreenViews(route: string, signedIn: boolean, send: ScreenViewSender): void {
  const lastRoute = useRef<string | null>(null);
  useEffect(() => {
    if (!signedIn || route === lastRoute.current) return;
    lastRoute.current = route;
    send(route);
  }, [signedIn, route, send]);
}
