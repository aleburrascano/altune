import type { SaveQueueStateRequest } from '@shared/apiClient/playback';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import { orderedQueueTracks, type QueueStore } from '@shared/playback/queueStore';

import { libraryTrackId } from '@features/playback/domain/playerStatus';

const MAX_SAVED_QUEUE_IDS = 10_000;

function libraryIds(tracks: readonly PlaybackTrack[]): string[] {
  const ids: string[] = [];
  for (const t of tracks) {
    const id = libraryTrackId(t);
    if (id) ids.push(id);
  }
  return ids;
}

type SaveSource = Pick<QueueStore, 'tracks' | 'playOrder' | 'currentIndex' | 'currentTrack'>;

function savedCurrentIndex(s: SaveSource): number {
  const current = s.currentTrack();
  if (!current || current.source.kind !== 'library') return 0;
  const before = s.playOrder.slice(0, s.currentIndex);
  return libraryIds(orderedQueueTracks({ tracks: s.tracks, playOrder: before })).length;
}

function windowStart(currentIndex: number, length: number): number {
  const centered = currentIndex - Math.floor(MAX_SAVED_QUEUE_IDS / 2);
  return Math.max(Math.min(centered, length - MAX_SAVED_QUEUE_IDS), 0);
}

function naturalWithin(natural: readonly string[], window: readonly string[]): string[] {
  const left = new Map<string, number>();
  for (const id of window) left.set(id, (left.get(id) ?? 0) + 1);
  return natural.filter((id) => {
    const count = left.get(id) ?? 0;
    left.set(id, count - 1);
    return count > 0;
  });
}

function windowOf(ordered: readonly string[], currentIndex: number) {
  const start = windowStart(currentIndex, ordered.length);
  return { ids: ordered.slice(start, start + MAX_SAVED_QUEUE_IDS), start };
}

type SavedLists = Pick<SaveQueueStateRequest, 'track_ids' | 'natural_order' | 'current_index'>;

export function savedQueueLists(s: SaveSource): SavedLists {
  const currentIndex = savedCurrentIndex(s);
  const { ids, start } = windowOf(libraryIds(orderedQueueTracks(s)), currentIndex);
  return {
    track_ids: ids,
    natural_order: naturalWithin(libraryIds(s.tracks), ids),
    current_index: currentIndex - start,
  };
}
