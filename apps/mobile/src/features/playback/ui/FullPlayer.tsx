import { useMemo } from 'react';
import { StyleSheet, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { ChevronDown, ListMusic, Pause, Play, Repeat, Repeat1, RotateCcw, Shuffle, SkipBack, SkipForward } from 'lucide-react-native';

import { useQueueStore } from '@shared/playback/queueStore';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Scrubber } from './Scrubber';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { Button } from '@shared/ui/primitives/Button';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

const RESTART_THRESHOLD_MS = 3_000;

export function FullPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, seekTo, retry } = usePlayback();
  const { skipToNext, skipToPrevious } = useQueuePlayback();
  const shuffled = useQueueStore((s) => s.shuffled);
  const repeatMode = useQueueStore((s) => s.repeatMode);
  const toggleShuffle = useQueueStore((s) => s.toggleShuffle);
  const cycleRepeatMode = useQueueStore((s) => s.cycleRepeatMode);
  const hasNext = useQueueStore((s) => s.hasNext());
  const hasPrevious = useQueueStore((s) => s.hasPrevious());
  const queueLength = useQueueStore((s) => s.playOrder.length);
  const theme = useTheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width: screenWidth } = useWindowDimensions();
  const artworkSize = screenWidth - spacing['3xl'] * 2;
  const shadowStyle = useMemo(
    () => ({ boxShadow: `0 8px 24px ${theme.color.accent}59` }),
    [theme.color.accent],
  );

  if (!track) {
    return null;
  }

  const isPlaying = status === 'playing';
  const isPreview = track.source.kind === 'preview';
  const isError = status === 'error';
  const isEnded = status === 'ended';

  const handlePrevious = () => {
    if (positionMs > RESTART_THRESHOLD_MS) {
      seekTo(0);
    } else {
      skipToPrevious();
    }
  };

  const handlePlayPause = () => {
    if (isEnded) {
      seekTo(0);
      resume();
    } else if (isPlaying) {
      pause();
    } else {
      resume();
    }
  };

  const statusLabel = isError
    ? 'Error'
    : isEnded
      ? isPreview ? 'Preview ended' : 'Finished'
      : isPreview ? 'Preview' : 'Now Playing';

  const statusTone = isError ? 'danger' : isPreview || isEnded ? 'warning' : 'secondary';

  const dimColor = theme.color.textTertiary;
  const activeColor = theme.color.accent;

  const RepeatIcon = repeatMode === 'one' ? Repeat1 : Repeat;
  const repeatColor = repeatMode === 'off' ? dimColor : activeColor;

  return (
    <View style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}>
      <View style={styles.header}>
        <IconButton
          icon={ChevronDown}
          size={28}
          onPress={() => router.back()}
          accessibilityLabel="Close player"
        />
        <Text variant="caption" tone={statusTone}>
          {statusLabel}
        </Text>
        {queueLength > 1 ? (
          <IconButton
            icon={ListMusic}
            size={22}
            onPress={() => router.push('/player/queue')}
            accessibilityLabel="View queue"
          />
        ) : (
          <View style={styles.headerSpacer} />
        )}
      </View>

      <View style={styles.artworkContainer}>
        <View style={[styles.artworkShadow, shadowStyle]}>
          <Artwork uri={track.artworkUrl} size={artworkSize} radius={radius.lg} />
        </View>
      </View>

      <View style={styles.info}>
        <Text variant="displayL" numberOfLines={2}>
          {track.title}
        </Text>
        <Text variant="body" tone="secondary" numberOfLines={1}>
          {track.artist}
        </Text>
      </View>

      <Scrubber positionMs={positionMs} durationMs={durationMs} onSeek={seekTo} />

      {isError ? (
        <View style={styles.errorControls}>
          <Button
            label="Retry"
            onPress={retry}
            haptic
          />
        </View>
      ) : isPreview ? (
        <View style={styles.controls}>
          <View style={styles.controlSpacer} />
          <View style={[styles.playButton, { backgroundColor: theme.color.accent }]}>
            <IconButton
              icon={isPlaying ? Pause : isEnded ? RotateCcw : Play}
              size={32}
              color={theme.color.onAccent}
              onPress={handlePlayPause}
              accessibilityLabel={isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play'}
            />
          </View>
          <View style={styles.controlSpacer} />
        </View>
      ) : (
        <View style={styles.controls}>
          <IconButton
            icon={Shuffle}
            size={20}
            color={shuffled ? activeColor : dimColor}
            onPress={toggleShuffle}
            accessibilityLabel={shuffled ? 'Disable shuffle' : 'Enable shuffle'}
          />
          <IconButton
            icon={SkipBack}
            size={24}
            color={hasPrevious || positionMs > RESTART_THRESHOLD_MS ? theme.color.textPrimary : dimColor}
            onPress={handlePrevious}
            accessibilityLabel="Previous track"
          />
          <View style={[styles.playButton, { backgroundColor: theme.color.accent }]}>
            <IconButton
              icon={isPlaying ? Pause : isEnded ? RotateCcw : Play}
              size={32}
              color={theme.color.onAccent}
              onPress={handlePlayPause}
              accessibilityLabel={isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play'}
            />
          </View>
          <IconButton
            icon={SkipForward}
            size={24}
            color={hasNext ? theme.color.textPrimary : dimColor}
            onPress={skipToNext}
            accessibilityLabel="Next track"
          />
          <IconButton
            icon={RepeatIcon}
            size={20}
            color={repeatColor}
            onPress={cycleRepeatMode}
            accessibilityLabel={`Repeat: ${repeatMode}`}
          />
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.lg,
  },
  headerSpacer: {
    width: 44,
  },
  artworkContainer: {
    alignItems: 'center',
    paddingHorizontal: spacing['3xl'],
    paddingBottom: spacing['3xl'],
  },
  artworkShadow: {
    elevation: 16,
  },
  info: {
    paddingHorizontal: spacing['2xl'],
    paddingBottom: spacing.xl,
    gap: spacing.xs,
  },
  controls: {
    flexDirection: 'row',
    justifyContent: 'center',
    alignItems: 'center',
    paddingTop: spacing['2xl'],
    gap: spacing.xl,
  },
  controlSpacer: {
    width: 44,
  },
  errorControls: {
    alignItems: 'center',
    paddingTop: spacing['2xl'],
  },
  playButton: {
    width: 64,
    height: 64,
    borderRadius: 32,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
