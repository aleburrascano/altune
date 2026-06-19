import { apiFetch } from './index';

export interface QueueStateResponse {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source_id: string;
}

export interface SaveQueueStateRequest {
  track_ids: string[];
  current_index: number;
  position_ms: number;
  shuffled: boolean;
  repeat_mode: string;
  source_id: string;
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
