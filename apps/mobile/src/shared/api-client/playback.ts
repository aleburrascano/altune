import { apiFetch } from './index';

// Display-ready snapshot of the currently-playing track, embedded by the server
// so now-playing renders from this one small call — no full library rehydrate.
export interface QueueStateCurrentTrack {
  id: string;
  title: string;
  artist: string;
  artwork_url: string | null;
  duration_seconds: number | null;
  acquisition_status: string;
}

export interface QueueStateResponse {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source_id: string;
  // Same tracks as track_ids but in pre-shuffle (album/playlist) order. Lets
  // restore rebuild the exact shuffled sequence and un-shuffle back to the
  // original order. Empty for older rows (client falls back to track_ids order).
  natural_order: string[];
  current_track?: QueueStateCurrentTrack;
}

export interface SaveQueueStateRequest {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source_id: string;
  natural_order: string[];
}

export async function getQueueState(): Promise<QueueStateResponse> {
  return apiFetch<QueueStateResponse>('/v1/playback/queue-state');
}

export async function saveQueueState(body: SaveQueueStateRequest): Promise<void> {
  await apiFetch<void>('/v1/playback/queue-state', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
