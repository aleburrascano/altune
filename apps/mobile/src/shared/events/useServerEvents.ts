import { useEffect, useRef } from 'react';
import { AppState } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { apiBase } from '../api-client';
import { supabase } from '../auth/supabaseClient';
import { isLoopEnabled, onKillSwitchChange } from '../killSwitch/killSwitch';
import { applyServerEvent } from './applyServerEvent';
import { SSEClient } from './sse-client';
import type { ServerEvent } from './sse-client';

async function getAccessToken(): Promise<string | null> {
  try {
    const { data } = await supabase.auth.getSession();
    return data.session?.access_token ?? null;
  } catch {
    return null;
  }
}

/** The slice of SSEClient the hook drives; a fake only needs these three methods. */
export type ServerEventsClient = Pick<SSEClient, 'connect' | 'disconnect' | 'dispose'>;

/** Builds the transport from SSEClient's own constructor arguments, so a fake stays type-checked against the real signature. */
export type ServerEventsClientFactory = (
  ...args: ConstructorParameters<typeof SSEClient>
) => ServerEventsClient;

const createSSEClient: ServerEventsClientFactory = (...args) => new SSEClient(...args);

export function useServerEvents(createClient: ServerEventsClientFactory = createSSEClient): void {
  const queryClient = useQueryClient();
  const clientRef = useRef<ServerEventsClient | null>(null);

  useEffect(() => {
    const url = `${apiBase}/v1/events`;

    const handleEvent = (event: ServerEvent): void => {
      applyServerEvent(queryClient, event);
    };

    const handleError = (error: unknown): void => {
      console.warn('[sse]', error);
    };

    const client = createClient(url, getAccessToken, handleEvent, handleError);
    clientRef.current = client;

    // The remote kill switch gates every connect; switching it off drops the live stream and its
    // pending reconnect, and switching it back on reconnects if the app is in the foreground.
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
      else if (AppState.currentState === 'active') connectIfEnabled();
    });

    return () => {
      unsubscribeKillSwitch();
      subscription.remove();
      client.dispose();
      clientRef.current = null;
    };
  }, [queryClient, createClient]);
}
