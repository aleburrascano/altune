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

/** The slice of SSEClient the hook drives; a fake only needs these three methods. */
export type ServerEventsClient = Pick<SSEClient, 'connect' | 'disconnect' | 'dispose'>;

/** Builds the transport from SSEClient's own constructor arguments, so a fake stays type-checked against the real signature. */
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
      else if (isForegrounded()) connectIfEnabled();
    });

    // A stream is authenticated once, when it is opened, and its events patch query keys and
    // stores that carry no user. So an identity change replaces the client instead of
    // reconnecting it: reconnecting would resend the previous account's Last-Event-ID, and
    // resume its half-parsed buffer, under the next account's token (#1772).
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
