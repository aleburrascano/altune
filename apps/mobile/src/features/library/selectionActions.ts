import { Download, ListEnd, ListPlus, Trash2, XCircle, type LucideIcon } from 'lucide-react-native';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import type { PinBatchResult, PinnedEntry, UnpinBatchResult } from '@shared/offline/pinnedStore';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import type { PlaybackTrack } from '@shared/playback/types';

import { reportPinBatch, reportUnpinBatch } from './pinBatchSummary';

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
    pinnedEntries: Record<string, PinnedEntry>;
    pinMany: (trackIds: TrackId[]) => Promise<PinBatchResult>;
    unpinMany: (trackIds: TrackId[]) => Promise<UnpinBatchResult>;
    queue: { addToQueueMany: (tracks: readonly PlaybackTrack[]) => void };
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
      disabled: selected.length === 0,
      onPress: opts.onAddToPlaylist,
    },
    {
      key: 'offline',
      label: allPinned ? 'Remove download' : 'Download',
      icon: allPinned ? XCircle : Download,
      disabled: ready.length === 0,
      onPress: () => {
        // The selection closes now; the summary arrives when the batch settles. Both directions
        // go out as one bounded batch: a "select all" reaches the thousands (#1699).
        const trackIds = ready.map((t) => t.id);
        if (allPinned) void opts.unpinMany(trackIds).then(reportUnpinBatch);
        else void opts.pinMany(trackIds).then(reportPinBatch);
        opts.onDone();
      },
    },
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
