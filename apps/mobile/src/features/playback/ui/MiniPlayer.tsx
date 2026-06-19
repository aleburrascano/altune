import { useEffect, useRef } from 'react';
import { Animated, Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Pause, Play, RotateCcw, SkipForward } from 'lucide-react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { usePlayback } from '@shared/playback/usePlayback';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

export function MiniPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, retry, errorMessage } = usePlayback();
  const { skipToNext } = useQueuePlayback();
  const showSkipNext = useQueueStore((s) => s.hasNext());
  const theme = useTheme();
  const router = useRouter();

  const progressAnim = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    const target = durationMs > 0 ? positionMs / durationMs : 0;
    Animated.timing(progressAnim, {
      toValue: target,
      duration: 200,
      useNativeDriver: false,
    }).start();
  }, [positionMs, durationMs, progressAnim]);

  if (!track || status === 'idle') {
    return null;
  }

  const isPlaying = status === 'playing';
  const isError = status === 'error';
  const isEnded = status === 'ended';
  const isPreview = track.source.kind === 'preview';

  const progressWidth = progressAnim.interpolate({
    inputRange: [0, 1],
    outputRange: ['0%', '100%'],
  });

  const onControlPress = isError
    ? retry
    : isPlaying
      ? pause
      : resume;

  const controlIcon = isError
    ? RotateCcw
    : isPlaying
      ? Pause
      : Play;

  const controlLabel = isError
    ? 'Retry'
    : isPlaying
      ? 'Pause'
      : 'Play';

  return (
    <Pressable
      onPress={() => router.push('/player')}
      style={[styles.container, { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border }]}
      accessibilityRole="button"
      accessibilityLabel={isPreview ? `Preview: ${track.title} by ${track.artist}` : `Now playing: ${track.title} by ${track.artist}`}
    >
      <View style={[styles.progressTrack, { backgroundColor: theme.color.border }]}>
        <Animated.View
          style={[
            styles.progressFill,
            { width: progressWidth, backgroundColor: theme.color.accent },
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
            {isError
              ? (errorMessage ?? 'Playback error')
              : isEnded
                ? isPreview ? 'Preview ended' : 'Finished'
                : isPreview ? `${track.artist} · Preview` : track.artist}
          </Text>
        </View>
        <IconButton
          icon={controlIcon}
          size={22}
          onPress={onControlPress}
          accessibilityLabel={controlLabel}
        />
        {showSkipNext && !isPreview ? (
          <IconButton
            icon={SkipForward}
            size={18}
            onPress={skipToNext}
            accessibilityLabel="Next track"
          />
        ) : null}
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
