import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Tappable, Text, spacing, formatDuration } from '@shared/ui';
import type { DiscoveryResult } from '@shared/catalog';
import { trackExtras } from '@features/detail/core/trackExtras';
import type { OwnedTrack } from '@features/detail/core/ownedTrack';
import { sharedStyles } from '@features/detail/view/detailStyles';
import { TrackSaveControl } from '@features/detail/view/SaveControls';

type AlbumTrackRowProps = {
  track: DiscoveryResult;
  index: number;
  subtitle: string;
  owned: OwnedTrack | null;
  onPress: () => void;
  onQuickSave: () => void;
  savingInBatch?: boolean;
};

type RowPlace = { position: number; duration: string | null };

function rowPositionAndDuration(track: DiscoveryResult, index: number): RowPlace {
  const te = trackExtras(track.extras);
  const position = te.trackPosition ?? index + 1;
  const duration = te.durationSeconds != null ? formatDuration(te.durationSeconds) : null;
  return { position, duration };
}

function rowLabel(position: number, title: string, duration: string | null): string {
  return `Track ${position}: ${title}${duration ? `, ${duration}` : ''}`;
}

type RowSubtitleProps = { subtitle: string };

function RowSubtitle({ subtitle }: RowSubtitleProps): ReactElement | null {
  return subtitle.length > 0 ? (
    <Text variant="label" tone="secondary" numberOfLines={1}>
      {subtitle}
    </Text>
  ) : null;
}

type RowDurationProps = { duration: string | null };

function RowDuration({ duration }: RowDurationProps): ReactElement | null {
  return duration ? (
    <Text variant="label" tone="tertiary" style={styles.duration}>
      {duration}
    </Text>
  ) : null;
}

export function AlbumTrackRow({
  track,
  index,
  subtitle,
  owned,
  onPress,
  onQuickSave,
  savingInBatch = false,
}: AlbumTrackRowProps): ReactElement {
  const { position, duration } = rowPositionAndDuration(track, index);

  return (
    <Tappable
      action="detail.album.track"
      testID={`detail-track-${index}`}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={rowLabel(position, track.title, duration)}
      style={({ pressed }) => [sharedStyles.trackRow, pressed ? sharedStyles.pressed : null]}
    >
      <Text variant="label" tone="tertiary" style={styles.position}>
        {position}
      </Text>
      <View style={sharedStyles.trackInfo}>
        <Text variant="body" numberOfLines={1}>
          {track.title}
        </Text>
        <RowSubtitle subtitle={subtitle} />
      </View>
      <RowDuration duration={duration} />
      <TrackSaveControl
        testID={`detail-track-save-${index}`}
        owned={owned}
        title={track.title}
        artist={track.subtitle}
        savingInBatch={savingInBatch}
        onPress={onQuickSave}
      />
    </Tappable>
  );
}

const POSITION_COLUMN_WIDTH = 24;

const styles = StyleSheet.create({
  position: { width: POSITION_COLUMN_WIDTH, textAlign: 'center' },
  duration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
});
