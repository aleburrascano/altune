import type { ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { formatDuration } from '@shared/lib/format';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { Row, Text, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { LibraryRowMoreButton } from './LibraryRowMoreButton';
import { LibraryRowPinnedIcon } from './LibraryRowPinnedIcon';
import { rowSurfaceStyle } from './libraryRowSurface';

import type { TrackResponse } from '@shared/api-client/types';

/** The library row outside selection mode: it plays or opens the track and offers its menu. */
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
    <Pressable
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
    </Pressable>
  );
}

const styles = StyleSheet.create({
  trailing: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
});
