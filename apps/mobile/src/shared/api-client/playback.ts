import { apiFetch } from './index';

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
