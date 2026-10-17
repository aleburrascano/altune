import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { formatDuration } from '@shared/ui/format/displayFormat';

import { trackExtras } from '@features/detail/domain/trackExtras';
import type { OwnedTrack } from '@features/detail/domain/ownedTrack';
import { sharedStyles } from '@features/detail/ui/detailStyles';
import { TrackSaveControl } from '@features/detail/ui/SaveControls';

type AlbumTrackRowProps = {
  track: DiscoveryResult;
  index: number;
  subtitle: string;
  owned: OwnedTrack | null;
  onPress: () => void;
  onQuickSave: () => void;
  savingInBatch?: boolean;
};

export function AlbumTrackRow({
  track,
  index,
  subtitle,
  owned,
  onPress,
  onQuickSave,
  savingInBatch = false,
}: AlbumTrackRowProps): ReactElement {
  const te = trackExtras(track.extras);
  const position = te.trackPosition ?? index + 1;
  const duration = te.durationSeconds != null ? formatDuration(te.durationSeconds) : null;

  return (
    <Tappable
      action="detail.album.track"
      testID={`detail-track-${index}`}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Track ${position}: ${track.title}${duration ? `, ${duration}` : ''}`}
      style={({ pressed }) => [sharedStyles.trackRow, pressed ? sharedStyles.pressed : null]}
    >
      <Text variant="label" tone="tertiary" style={styles.position}>
        {position}
      </Text>
      <View style={sharedStyles.trackInfo}>
        <Text variant="body" numberOfLines={1}>
          {track.title}
        </Text>
        {subtitle.length > 0 ? (
          <Text variant="label" tone="secondary" numberOfLines={1}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      {duration ? (
        <Text variant="label" tone="tertiary" style={styles.duration}>
          {duration}
        </Text>
      ) : null}
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
