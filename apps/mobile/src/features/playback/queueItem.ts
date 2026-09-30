import type { FeaturedArtist } from '@shared/api-client/types';
import type { PlaybackTrack, QueueSource } from '@shared/playback/types';

export type QueueItem = {
  trackIndex: number;
  queueIndex: number;
  title: string;
  artist: string;
  artworkUrl: string | null;
  durationSeconds: number | undefined;
  featuredArtists: readonly FeaturedArtist[] | undefined;
};

export function formatSeconds(sec: number | undefined): string {
  if (sec == null || sec === 0) return '';
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${m}:${String(s).padStart(2, '0')}`;
}

export function queueSourceLabel(source: QueueSource | null): string {
  if (!source) return 'Queue';
  if (source.kind === 'playlist') return `Playing from ${source.name}`;
  if (source.kind === 'library') return 'Playing from Library';
  return 'Playing from search';
}

function toQueueItem(t: PlaybackTrack, trackIndex: number, queueIndex: number): QueueItem {
  const { title, artist, artworkUrl, durationSeconds, featuredArtists } = t;
  return { trackIndex, queueIndex, title, artist, artworkUrl, durationSeconds, featuredArtists };
}

export function buildUpNextItems(
  tracks: readonly PlaybackTrack[],
  playOrder: readonly number[],
  currentIndex: number,
): QueueItem[] {
  return playOrder.flatMap((trackIdx, queueIdx) => {
    const t = tracks[trackIdx];
    return queueIdx > currentIndex && t ? [toQueueItem(t, trackIdx, queueIdx)] : [];
  });
}
