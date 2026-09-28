import { useEffect } from 'react';
import { AppState } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { apiBase } from '../api-client';
import { withinAuthDeadline } from '../auth/authDeadline';
import { supabase } from '../auth/supabaseClient';
import { isLoopEnabled, onKillSwitchChange } from '../killSwitch/killSwitch';
import { onSignOut } from '../session/signOutCleanup';
import { applyServerEvent } from './applyServerEvent';
import { SSEClient } from './sse-client';
import type { ServerEvent } from './sse-client';

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
  return AppState.currentState === 'active';
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

    const subscription = AppState.addEventListener('change', (nextState) => {
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
      subscription.remove();
      client.dispose();
    };
  }, [queryClient, createClient]);
}
