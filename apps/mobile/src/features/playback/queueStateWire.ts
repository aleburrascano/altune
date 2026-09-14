import { asPlaylistId } from '@shared/api-client/ids';
import type { QueueSourceWire } from '@shared/api-client/playback';
import type { QueueSource, RepeatMode } from '@shared/playback/types';

export function toWireSource(source: QueueSource | null): QueueSourceWire | null {
  if (!source) return null;
  if (source.kind === 'playlist') {
    return { kind: 'playlist', playlist_id: source.playlistId, name: source.name };
  }
  if (source.kind === 'search') return { kind: 'search', query: source.query };
  return { kind: 'library' };
}

export function fromWireSource(source: QueueSourceWire | null | undefined): QueueSource | null {
  if (!source) return null;
  if (source.kind === 'playlist') {
    return {
      kind: 'playlist',
      playlistId: asPlaylistId(source.playlist_id ?? ''),
      name: source.name ?? '',
    };
  }
  if (source.kind === 'search') return { kind: 'search', query: source.query ?? '' };
  return { kind: 'library' };
}

export function asRepeatMode(value: unknown): RepeatMode | null {
  return value === 'off' || value === 'all' || value === 'one' ? value : null;
}
