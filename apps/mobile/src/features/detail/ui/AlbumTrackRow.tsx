import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Check, Plus } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { formatDuration } from '../extras';
import { trackExtras } from '../extras-accessors';
import { sharedStyles } from './helpers';

type AlbumTrackRowProps = {
  track: DiscoveryResult;
  index: number;
  subtitle: string;
  isSaved: boolean;
  onPress: () => void;
  onQuickSave: () => void;
};

export function AlbumTrackRow({
  track,
  index,
  subtitle,
  isSaved,
  onPress,
  onQuickSave,
}: AlbumTrackRowProps): ReactElement {
  const theme = useTheme();
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
      <Pressable
        testID={`detail-track-save-${index}`}
        onPress={(e) => { e.stopPropagation(); onQuickSave(); }}
        disabled={isSaved}
        accessibilityRole="button"
        accessibilityLabel={isSaved ? `${track.title} saved` : `Save ${track.title}`}
        hitSlop={8}
        style={styles.saveBtn}
      >
        {isSaved ? (
          <Check size={16} color={theme.color.success} />
        ) : (
          <Plus size={16} color={theme.color.accent} />
        )}
      </Pressable>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  position: { width: 24, textAlign: 'center' },
  duration: { marginRight: spacing.xs },
  saveBtn: {
    minWidth: 44,
    minHeight: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
