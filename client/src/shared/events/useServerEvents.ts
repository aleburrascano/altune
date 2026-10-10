import { useEffect } from 'react';
import { isAppActive, subscribeAppState } from '@shared/lifecycle/appState';
import { useQueryClient } from '@tanstack/react-query';

import { apiBase } from '@shared/api-client/config';
import { withinAuthDeadline } from '@shared/auth/authDeadline';
import { supabase } from '@shared/auth/supabaseClient';
import { isLoopEnabled, onKillSwitchChange } from '@shared/killSwitch/killSwitch';
import { onSignOut } from '@shared/session/signOutCleanup';
import { applyServerEvent } from '@shared/events/applyServerEvent';
import { SSEClient } from '@shared/events/sseClient';
import type { ServerEvent } from '@shared/events/sseFrames';

async function getAccessToken(): Promise<string | null> {
  try {
    const { data: stored } = await withinAuthDeadline(
      supabase.auth.getSession(),
      'event stream auth lookup',
    );
    return stored.session?.access_token ?? null;
  } catch {
    return null;
  }
}

function isForegrounded(): boolean {
  return isAppActive();
}

export type ServerEventsClient = Pick<SSEClient, 'connect' | 'disconnect' | 'dispose'>;

export type ServerEventsClientFactory = (
  ...args: ConstructorParameters<typeof SSEClient>
) => ServerEventsClient;

const createSSEClient: ServerEventsClientFactory = (...args) => new SSEClient(...args);

export function useServerEvents(createClient: ServerEventsClientFactory = createSSEClient): void {
  const queryClient = useQueryClient();

  useEffect(() => {
    const url = `${apiBase}/v1/events`;

    const handleEvent = (event: ServerEvent): void => {
      applyServerEvent(queryClient, event);
    };

    const handleError = (error: unknown): void => {
      console.warn('[sse]', error);
    };

    const openClient = (): ServerEventsClient =>
      createClient(url, getAccessToken, handleEvent, handleError);

    let client = openClient();

    const connectIfEnabled = (): void => {
      if (isLoopEnabled('serverEvents')) void client.connect();
    };
    connectIfEnabled();

    const unsubscribeAppState = subscribeAppState((nextState) => {
      if (nextState === 'active') {
        connectIfEnabled();
      } else {
        client.disconnect();
      }
    });

    const unsubscribeKillSwitch = onKillSwitchChange((loop, enabled) => {
      if (loop !== 'serverEvents') return;
      if (!enabled) client.disconnect();
      else if (isForegrounded()) connectIfEnabled();
    });

    const unregisterSignOutCleanup = onSignOut(() => {
      client.dispose();
      client = openClient();
      if (isForegrounded()) connectIfEnabled();
    });

    return () => {
      unregisterSignOutCleanup();
      unsubscribeKillSwitch();
      unsubscribeAppState();
      client.dispose();
    };
  }, [queryClient, createClient]);
}
