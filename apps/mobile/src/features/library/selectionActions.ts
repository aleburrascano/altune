import { Download, ListEnd, ListPlus, Trash2, XCircle, type LucideIcon } from 'lucide-react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';

import { reportPinBatch, reportUnpinBatch } from './pinBatchSummary';
import { offlineEligibility, type LibraryOffline } from './hooks/useLibraryOffline';

export type SelectionAction = {
  key: string;
  label: string;
  icon: LucideIcon;
  onPress: () => void;
  tone?: 'danger';
  disabled?: boolean;
};

export function buildSelectionActions(
  selected: TrackResponse[],
  opts: {
    offline: LibraryOffline;
    queue: { addToQueueMany: (tracks: readonly PlaybackTrack[]) => void };
    onAddToPlaylist: () => void;
    onDone: () => void;
    danger: { label: string; onPress: () => void };
  },
): SelectionAction[] {
  const ready = selected.filter((t) => t.acquisition_status === 'ready');
  const { downloadableIds, allPinned } = offlineEligibility(selected, opts.offline.statusOf);

  return [
    {
      key: 'playlist',
      label: 'Add to Playlist',
      icon: ListPlus,
      disabled: selected.length === 0,
      onPress: opts.onAddToPlaylist,
    },
    ...(opts.offline.supported
      ? [
          {
            key: 'offline',
            label: allPinned ? 'Remove download' : 'Download',
            icon: allPinned ? XCircle : Download,
            disabled: ready.length === 0,
            onPress: () => {
              if (allPinned) void opts.offline.unpinMany(downloadableIds).then(reportUnpinBatch);
              else void opts.offline.pinMany(downloadableIds).then(reportPinBatch);
              opts.onDone();
            },
          },
        ]
      : []),
    {
      key: 'queue',
      label: 'Add to Queue',
      icon: ListEnd,
      disabled: ready.length === 0,
      onPress: () => {
        opts.queue.addToQueueMany(ready.map((t) => toPlaybackTrack(t)));
        opts.onDone();
      },
    },
    {
      key: 'danger',
      label: opts.danger.label,
      icon: Trash2,
      tone: 'danger',
      disabled: selected.length === 0,
      onPress: opts.danger.onPress,
    },
  ];
}
