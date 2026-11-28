import { Download, ListEnd, ListPlus, Trash2, XCircle, type LucideIcon } from 'lucide-react-native';
import type { TrackId, TrackResponse } from '@shared/catalog';
import { toPlaybackTrack, type PlaybackTrack } from '@shared/playback';
import {
  offlineEligibility,
  reportPinBatch,
  reportUnpinBatch,
  type LibraryOffline,
} from '@features/library/view/useLibraryOffline';
import type { DangerAction } from '@features/library/view/trackMenuItems';
import type { ShowFailure } from '@features/library/core/errors';
import { isReady } from '@shared/catalog';

export type SelectionAction = {
  key: string;
  label: string;
  icon: LucideIcon;
  onPress: () => void;
  tone?: 'danger';
  disabled?: boolean;
};

type SelectionQueue = { addToQueueMany: (tracks: readonly PlaybackTrack[]) => void };

type SelectionActionOptions = {
  offline: LibraryOffline;
  queue: SelectionQueue;
  onAddToPlaylist: () => void;
  onDone: () => void;
  danger: DangerAction;
  showFailure: ShowFailure;
};

type OfflineEligibility = ReturnType<typeof offlineEligibility>;

function playlistAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  return {
    key: 'playlist',
    label: 'Add to Playlist',
    icon: ListPlus,
    disabled: selected.length === 0,
    onPress: opts.onAddToPlaylist,
  };
}

function removeDownloads(trackIds: TrackId[], opts: SelectionActionOptions): void {
  void opts.offline.unpinMany(trackIds).then((batch) => reportUnpinBatch(opts.showFailure, batch));
}

function addDownloads(trackIds: TrackId[], opts: SelectionActionOptions): void {
  void opts.offline.pinMany(trackIds).then((batch) => reportPinBatch(opts.showFailure, batch));
}

function pressOffline(eligibility: OfflineEligibility, opts: SelectionActionOptions): void {
  const { downloadableIds, allPinned } = eligibility;
  if (allPinned) removeDownloads(downloadableIds, opts);
  else addDownloads(downloadableIds, opts);
  opts.onDone();
}

function offlineAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  const eligibility = offlineEligibility(selected, opts.offline.statusOf);
  return {
    key: 'offline',
    label: eligibility.allPinned ? 'Remove download' : 'Download',
    icon: eligibility.allPinned ? XCircle : Download,
    disabled: eligibility.downloadableIds.length === 0,
    onPress: () => pressOffline(eligibility, opts),
  };
}

function offlineActions(
  selected: TrackResponse[],
  opts: SelectionActionOptions,
): SelectionAction[] {
  if (!opts.offline.supported) return [];
  return [offlineAction(selected, opts)];
}

function queueReady(ready: TrackResponse[], opts: SelectionActionOptions): void {
  opts.queue.addToQueueMany(ready.map((t) => toPlaybackTrack(t)));
  opts.onDone();
}

function queueAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  const ready = selected.filter((t) => isReady(t.acquisition_status));
  return {
    key: 'queue',
    label: 'Add to Queue',
    icon: ListEnd,
    disabled: ready.length === 0,
    onPress: () => queueReady(ready, opts),
  };
}

function dangerAction(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction {
  return {
    key: 'danger',
    label: opts.danger.label,
    icon: Trash2,
    tone: 'danger',
    disabled: selected.length === 0,
    onPress: opts.danger.onPress,
  };
}

function leadActions(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction[] {
  return [playlistAction(selected, opts), ...offlineActions(selected, opts)];
}

function tailActions(selected: TrackResponse[], opts: SelectionActionOptions): SelectionAction[] {
  return [queueAction(selected, opts), dangerAction(selected, opts)];
}

export function buildSelectionActions(
  selected: TrackResponse[],
  opts: SelectionActionOptions,
): SelectionAction[] {
  return [...leadActions(selected, opts), ...tailActions(selected, opts)];
}
