/**
 * useServerEvents — connects to the SSE endpoint and invalidates
 * React Query caches when domain events arrive from the server.
 *
 * Lifecycle:
 * - Connects when the app is in the foreground and authenticated
 * - Disconnects when backgrounded
 * - Reconnects automatically on foreground or connection loss
 * - Sends Last-Event-ID on reconnect for replay
 */

import { useEffect, useRef } from 'react';
import { AppState } from 'react-native';
import { useQueryClient } from '@tanstack/react-query';

import { apiBase } from '../api-client';
import { supabase } from '../auth/supabaseClient';
import { SSEClient } from './sse-client';
import type { ServerEvent } from './sse-client';

const EVENT_INVALIDATION_MAP: Record<string, string[][]> = {
  track_added_to_library: [['library-home'], ['library']],
  track_acquisition_completed: [['library-home'], ['library']],
  track_acquisition_failed: [['library-home'], ['library']],
  track_deleted: [['library-home'], ['library'], ['playlists']],
  playlist_created: [['playlists']],
  playlist_deleted: [['playlists'], ['playlist']],
  track_added_to_playlist: [['playlist'], ['playlists']],
  track_removed_from_playlist: [['playlist'], ['playlists']],
};

async function getAccessToken(): Promise<string | null> {
  try {
    const { data } = await supabase.auth.getSession();
    return data.session?.access_token ?? null;
  } catch {
    return null;
  }
}

export function useServerEvents(): void {
  const queryClient = useQueryClient();
  const clientRef = useRef<SSEClient | null>(null);

  useEffect(() => {
    const url = `${apiBase}/v1/events`;

    const handleEvent = (event: ServerEvent): void => {
      const keys = EVENT_INVALIDATION_MAP[event.type];
      if (!keys) return;
      for (const queryKey of keys) {
        void queryClient.invalidateQueries({ queryKey });
      }
    };

    const handleError = (): void => {
      // Reconnection is handled by SSEClient internally
    };

    const client = new SSEClient(url, getAccessToken, handleEvent, handleError);
    clientRef.current = client;
    void client.connect();

    const subscription = AppState.addEventListener('change', (nextState) => {
      if (nextState === 'active') {
        void client.connect();
      } else {
        client.disconnect();
      }
    });

    return () => {
      subscription.remove();
      client.dispose();
      clientRef.current = null;
    };
  }, [queryClient]);
}
