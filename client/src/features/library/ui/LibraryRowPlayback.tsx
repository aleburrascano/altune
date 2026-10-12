import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { formatDuration } from '@shared/ui/displayFormat';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { Row, Text, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import {
  LibraryRowMoreButton,
  LibraryRowPinnedIcon,
} from '@features/library/ui/LibraryRowAccessories';
import { rowSurfaceStyle } from '@features/library/ui/libraryRowStyle';
import type { TrackResponse } from '@shared/apiClient/trackResponse';

export function LibraryRowPlayback({
  track,
  a11yLabel,
  pinned,
  onPlay,
  onPress,
  onMore,
  onLongPress,
  children,
}: {
  track: TrackResponse;
  a11yLabel: string;
  pinned: PinnedStatus | undefined;
  onPlay: (() => void) | undefined;
  onPress: () => void;
  onMore: (anchor: MenuAnchor) => void;
  onLongPress: (() => void) | undefined;
  children: ReactNode;
}): ReactElement {
  const theme = useTheme();
  const duration =
    track.duration_seconds != null && track.duration_seconds > 0
      ? formatDuration(track.duration_seconds)
      : null;

  const playOrOpenTrack = () => {
    if (track.acquisition_status === 'ready' && onPlay) {
      onPlay();
      return;
    }
    onPress();
  };

  return (
    <Tappable
      action="library.row.open"
      trackId={track.id}
      testID={`library-row-${track.id}`}
      onPress={playOrOpenTrack}
      {...(onLongPress != null ? { onLongPress } : {})}
      accessibilityRole="button"
      accessibilityLabel={a11yLabel}
      style={rowSurfaceStyle(theme, null)}
    >
      <Row
        leading={
          <Artwork uri={track.artwork_url} size={48} radius={6} accessibilityLabel="Album art" />
        }
        trailing={
          <View style={styles.trailing}>
            <LibraryRowPinnedIcon trackId={track.id} status={pinned} />
            {duration != null ? (
              <Text variant="caption" tone="tertiary">
                {duration}
              </Text>
            ) : null}
            <LibraryRowMoreButton trackId={track.id} title={track.title} onMore={onMore} />
          </View>
        }
      >
        {children}
      </Row>
    </Tappable>
  );
}

const styles = StyleSheet.create({
  trailing: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
});
