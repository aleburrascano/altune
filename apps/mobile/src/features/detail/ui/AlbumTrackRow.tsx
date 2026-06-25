import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { formatDuration } from '../extras';
import { trackExtras } from '../extras-accessors';
import type { SaveControlState } from '../save-control-state';
import { sharedStyles } from './helpers';
import { TrackSaveControl } from './TrackSaveControl';

type AlbumTrackRowProps = {
  track: DiscoveryResult;
  index: number;
  subtitle: string;
  saveState: SaveControlState;
  onPress: () => void;
  onQuickSave: () => void;
};

export function AlbumTrackRow({
  track,
  index,
  subtitle,
  saveState,
  onPress,
  onQuickSave,
}: AlbumTrackRowProps): ReactElement {
  const te = trackExtras(track.extras);
  const position = te.trackPosition ?? index + 1;
  const duration = te.durationSeconds != null ? formatDuration(te.durationSeconds) : null;

  return (
    <Pressable
      testID={`detail-track-${index}`}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={`Track ${position}: ${track.title}${duration ? `, ${duration}` : ''}`}
      style={({ pressed }) => [sharedStyles.trackRow, pressed ? { opacity: 0.6 } : null]}
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
        state={saveState}
        title={track.title}
        onPress={onQuickSave}
      />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  position: { width: 24, textAlign: 'center' },
  duration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
});
