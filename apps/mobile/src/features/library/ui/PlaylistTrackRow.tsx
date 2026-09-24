import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import type { usePlayback } from '@shared/playback/usePlayback';
import { spacing } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { Selection } from '../hooks/useSelection';
import type { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { LibraryRow } from './LibraryRow';

type PlaylistTrackRowProps = {
  track: TrackResponse;
  selection: Selection;
  playback: ReturnType<typeof usePlayback>;
  retry: ReturnType<typeof useRetryAcquisition>;
  onPlay: () => void;
  onOpen: () => void;
  onMore: (anchor: MenuAnchor) => void;
};

export function PlaylistTrackRow({
  track,
  selection,
  playback,
  retry,
  onPlay,
  onOpen,
  onMore,
}: PlaylistTrackRowProps): ReactElement {
  return (
    <View style={styles.trackRow}>
      <LibraryRow
        track={track}
        {...(track.acquisition_status === 'ready' ? { onPlay } : {})}
        onPress={onOpen}
        onMore={onMore}
        onLongPress={() => selection.begin(track.id)}
        {...(selection.active
          ? {
              selectable: {
                selected: selection.has(track.id),
                onToggle: () => selection.toggle(track.id),
              },
            }
          : {})}
        {...(track.acquisition_status === 'failed'
          ? { onRetry: () => retry.mutate(track.id) }
          : {})}
        retrying={retry.isInFlight(track.id)}
        isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: track.id })}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  trackRow: { paddingHorizontal: spacing.lg },
});
