import { Download, ListEnd, ListPlus, Trash2, XCircle } from 'lucide-react-native';

import type { TrackResponse } from '@shared/api-client/types';
import type { PinnedEntry } from '@shared/offline/pinnedStore';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';

import type { SelectionAction } from './SelectionBar';

export function buildSelectionActions(
  selected: TrackResponse[],
  opts: {
    pinnedEntries: Record<string, PinnedEntry>;
    pinMany: (trackIds: string[]) => void;
    unpin: (trackId: string) => void;
    queue: { addToQueue: (track: PlaybackTrack) => void };
    onAddToPlaylist: () => void;
    onDone: () => void;
    danger: { label: string; onPress: () => void };
  },
): SelectionAction[] {
  const ready = selected.filter((t) => t.acquisition_status === 'ready');
  const allPinned =
    ready.length > 0 && ready.every((t) => opts.pinnedEntries[t.id]?.status === 'ready');

  return [
    {
      key: 'playlist',
      label: 'Add to Playlist',
      icon: ListPlus,
      onPress: opts.onAddToPlaylist,
    },
    {
      key: 'offline',
      label: allPinned ? 'Remove download' : 'Download',
      icon: allPinned ? XCircle : Download,
      disabled: ready.length === 0,
      onPress: () => {
        if (allPinned) {
          ready.forEach((t) => opts.unpin(t.id));
        } else {
          opts.pinMany(ready.map((t) => t.id));
        }
        opts.onDone();
      },
    },
    {
      key: 'queue',
      label: 'Add to Queue',
      icon: ListEnd,
      disabled: ready.length === 0,
      onPress: () => {
        ready.forEach((t) => opts.queue.addToQueue(toPlaybackTrack(t)));
        opts.onDone();
      },
    },
    {
      key: 'danger',
      label: opts.danger.label,
      icon: Trash2,
      tone: 'danger',
      onPress: opts.danger.onPress,
    },
  ];
}
