import { useEffect, useRef } from 'react';

import { useSession } from '@shared/auth/useSession';
import { useRouteSegments } from '@shared/navigation';
import { recordEvent } from '@shared/telemetry/recordEvent';

function sendScreenView(route: string): void {
  recordEvent({ type: 'screen_view', payload: { route } }).catch((error: unknown) => {
    console.warn('[telemetry] recording event failed', error);
  });
}

export function useScreenViews(): void {
  const route = '/' + useRouteSegments().join('/');
  const { status } = useSession();
  const lastRoute = useRef<string | null>(null);
  useEffect(() => {
    if (status !== 'signed-in' || route === lastRoute.current) return;
    lastRoute.current = route;
    sendScreenView(route);
  }, [status, route]);
}
