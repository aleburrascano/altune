import { apiFetch, apiSend } from './index';

export interface QueueStateCurrentTrack {
  id: string;
  title: string;
  artist: string;
  artwork_url: string | null;
  duration_seconds: number | null;
  acquisition_status: string;
}

export interface QueueSourceWire {
  kind: 'library' | 'playlist' | 'search';
  playlist_id?: string;
  name?: string;
  query?: string;
}

export interface QueueStateResponse {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source: QueueSourceWire | null;
  natural_order: string[];
  current_track?: QueueStateCurrentTrack;
  // Present (true) only when the server's now-playing lookup failed; an absent
  // current_track without it means there simply is no current track. Mirrors
  // the Go queueStateResponse.current_track_unavailable (json omitempty).
  current_track_unavailable?: boolean;
}

export interface SaveQueueStateRequest {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source: QueueSourceWire | null;
  natural_order: string[];
}

// The one queue-state body is parsed by features/playback/queueStateWire.ts
// before any restore step reads it, and that parser answers with a typed result
// rather than a throw so a row a newer client wrote costs the user their
// position, not their queue. A second parse here would re-narrow the same bytes
// and turn those recoverable rows into a failed resume (#1777).
export async function getQueueState(): Promise<QueueStateResponse> {
  return apiFetch<QueueStateResponse>('/v1/playback/queue-state');
}

export async function saveQueueState(body: SaveQueueStateRequest): Promise<void> {
  await apiSend<void>('/v1/playback/queue-state', 'PUT', body);
}
