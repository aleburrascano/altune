import type { FeaturedArtist } from '@shared/api-client/trackResponse';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import type { QueueSource } from '@shared/playback/queueOrder';
import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

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

export type QueueMenuContext = {
  currentIndex: number;
  queueLength: number;
  moveQueueItem: (from: number, to: number) => void;
  removeFromQueue: (queueIndex: number) => void;
};

function moveOptions(queueIndex: number, ctx: QueueMenuContext): ActionSheetOption[] {
  const firstUpNext = ctx.currentIndex + 1;
  const opts: ActionSheetOption[] = [];
  if (queueIndex !== firstUpNext) {
    opts.push({ label: 'Move to Top', onPress: () => ctx.moveQueueItem(queueIndex, firstUpNext) });
    opts.push({ label: 'Move Up', onPress: () => ctx.moveQueueItem(queueIndex, queueIndex - 1) });
  }
  if (queueIndex !== ctx.queueLength - 1) {
    opts.push({ label: 'Move Down', onPress: () => ctx.moveQueueItem(queueIndex, queueIndex + 1) });
  }
  return opts;
}

export function queueMenuOptions(queueIndex: number, ctx: QueueMenuContext): ActionSheetOption[] {
  return [
    ...moveOptions(queueIndex, ctx),
    {
      label: 'Remove from Queue',
      tone: 'danger',
      onPress: () => ctx.removeFromQueue(queueIndex),
    },
  ];
}
