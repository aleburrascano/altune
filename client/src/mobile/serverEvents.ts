import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';

import { supabase, useSession, withinAuthDeadline } from '@shared/auth';
import {
  SSEClient,
  startServerEvents,
  useScreenViews,
  type AppForeground,
  type ServerEvent,
  type StreamIO,
} from '@shared/events';
import { isAppActive, subscribeAppState } from '@shared/lifecycle';
import { warn } from '@shared/log';
import { useRouteSegments } from '@shared/navigation';
import { recordEvent } from '@shared/telemetry';

async function getAccessToken(): Promise<string | null> {
  const { data: stored } = await withinAuthDeadline(
    supabase.auth.getSession(),
    'event stream auth lookup',
  );
  return stored.session?.access_token ?? null;
}

const streamIO: StreamIO = {
  openRequest: () => new XMLHttpRequest(),
  random: () => Math.random(),
  warn,
};

function createSSEClient(
  url: string,
  getToken: () => Promise<string | null>,
  onEvent: (event: ServerEvent) => void,
  onError: (error: unknown) => void,
): SSEClient {
  return new SSEClient(url, getToken, onEvent, onError, streamIO);
}

const foreground: AppForeground = { isActive: isAppActive, subscribe: subscribeAppState };

function startAppServerEvents(cache: QueryClient): () => void {
  return startServerEvents({
    cache,
    createClient: createSSEClient,
    getToken: getAccessToken,
    foreground,
    warn,
  });
}

export function ServerEventsBridge(): null {
  const queryClient = useQueryClient();
  useEffect(() => startAppServerEvents(queryClient), [queryClient]);
  return null;
}

function sendScreenView(route: string): void {
  recordEvent({ type: 'screen_view', payload: { route } }).catch((error: unknown) => {
    warn('[telemetry] recording event failed', error);
  });
}

export function ScreenViewsBridge(): null {
  const route = '/' + useRouteSegments().join('/');
  const { status } = useSession();
  useScreenViews(route, status === 'signed-in', sendScreenView);
  return null;
}
