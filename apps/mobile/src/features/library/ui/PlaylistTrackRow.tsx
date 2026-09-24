import type { ComponentProps, ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import type { usePlayback } from '@shared/playback/usePlayback';
import { spacing } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { Selection } from '../hooks/useSelection';
import type { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { LibraryRow } from './LibraryRow';

export type PlaylistTrackRowProps = {
  track: TrackResponse;
  selection: Selection;
  playback: ReturnType<typeof usePlayback>;
  retry: ReturnType<typeof useRetryAcquisition>;
  onPlay: () => void;
  onOpen: () => void;
  onMore: (anchor: MenuAnchor) => void;
};

type LibraryRowProps = ComponentProps<typeof LibraryRow>;

function baseProps(p: PlaylistTrackRowProps): LibraryRowProps {
  return {
    track: p.track,
    onPress: p.onOpen,
    onMore: p.onMore,
    onLongPress: () => p.selection.begin(p.track.id),
    retrying: p.retry.isInFlight(p.track.id),
    isPlaying: isCurrentlyPlaying(p.playback, { kind: 'library', trackId: p.track.id }),
  };
}

function selectableProps(p: PlaylistTrackRowProps): Partial<LibraryRowProps> {
  if (!p.selection.active) return {};
  const { track, selection } = p;
  return {
    selectable: { selected: selection.has(track.id), onToggle: () => selection.toggle(track.id) },
  };
}

function statusProps(p: PlaylistTrackRowProps): Partial<LibraryRowProps> {
  if (p.track.acquisition_status === 'ready') return { onPlay: p.onPlay };
  if (p.track.acquisition_status !== 'failed') return {};
  return { onRetry: () => p.retry.mutate(p.track.id) };
}

function rowProps(p: PlaylistTrackRowProps): LibraryRowProps {
  return { ...baseProps(p), ...selectableProps(p), ...statusProps(p) };
}

export function PlaylistTrackRow(props: PlaylistTrackRowProps): ReactElement {
  return (
    <View style={styles.trackRow}>
      <LibraryRow {...rowProps(props)} />
    </View>
  );
}

const styles = StyleSheet.create({
  trackRow: { paddingHorizontal: spacing.lg },
});
