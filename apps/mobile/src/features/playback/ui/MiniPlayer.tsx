import { Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Pause, Play } from 'lucide-react-native';

import { usePlayback } from '../hooks/usePlayback';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

export function MiniPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, errorMessage } = usePlayback();
  const theme = useTheme();
  const router = useRouter();

  if (!track || status === 'idle') {
    return null;
  }

  const isPlaying = status === 'playing';
  const isError = status === 'error';
  const progress = durationMs > 0 ? positionMs / durationMs : 0;

  return (
    <Pressable
      onPress={() => router.push('/player')}
      style={[styles.container, { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border }]}
      accessibilityRole="button"
      accessibilityLabel={`Now playing: ${track.title} by ${track.artist}`}
    >
      <View style={[styles.progressTrack, { backgroundColor: theme.color.border }]}>
        <View
          style={[
            styles.progressFill,
            { width: `${progress * 100}%`, backgroundColor: theme.color.accent },
          ]}
        />
      </View>
      <View style={styles.content}>
        <Artwork uri={track.artworkUrl} size={44} radius={radius.sm} />
        <View style={styles.info}>
          <Text variant="label" numberOfLines={1}>
            {track.title}
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1}>
            {isError ? (errorMessage ?? 'Playback error') : track.artist}
          </Text>
        </View>
        <IconButton
          icon={isPlaying ? Pause : Play}
          size={22}
          onPress={isPlaying ? pause : resume}
          accessibilityLabel={isPlaying ? 'Pause' : 'Play'}
        />
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  container: {
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  progressTrack: {
    height: 2,
  },
  progressFill: {
    height: 2,
  },
  content: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.md,
  },
  info: {
    flex: 1,
    gap: 2,
  },
});
