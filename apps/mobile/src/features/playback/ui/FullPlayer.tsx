import { useMemo, useState } from 'react';
import { StyleSheet, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import {
  ChevronDown,
  ListMusic,
  Mic2,
  MoreHorizontal,
  Pause,
  Play,
  Repeat,
  Repeat1,
  RotateCcw,
  Shuffle,
  SkipBack,
  SkipForward,
} from 'lucide-react-native';

import { withFeaturing } from '@shared/lib/featured';
import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import type { PlaybackStatus } from '@shared/playback/types';
import { PlayerOptionsSheets } from './PlayerOptionsSheets';
import { Scrubber } from './Scrubber';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { Button } from '@shared/ui/primitives/Button';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

function getStatusDisplay(
  status: PlaybackStatus,
  isPreview: boolean,
): { label: string; tone: 'danger' | 'warning' | 'secondary' } {
  if (status === 'error') return { label: 'Error', tone: 'danger' };
  if (status === 'ended') {
    return { label: isPreview ? 'Preview ended' : 'Finished', tone: 'warning' };
  }
  return {
    label: isPreview ? 'Preview' : 'Now Playing',
    tone: isPreview ? 'warning' : 'secondary',
  };
}

function PlayButton({
  isPlaying,
  isEnded,
  onPress,
}: {
  isPlaying: boolean;
  isEnded: boolean;
  onPress: () => void;
}) {
  const theme = useTheme();
  return (
    <View style={[styles.playButton, { backgroundColor: theme.color.accent }]}>
      <IconButton
        icon={isPlaying ? Pause : isEnded ? RotateCcw : Play}
        size={32}
        color={theme.color.onAccent}
        onPress={onPress}
        accessibilityLabel={isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play'}
      />
    </View>
  );
}

export function FullPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, seekTo, retry } = usePlayback();
  const [optionsOpen, setOptionsOpen] = useState(false);
  const { skipToNext, skipToPrevious, toggleShuffle, cycleRepeatMode } = useQueuePlayback();
  const shuffled = useQueueStore((s) => s.shuffled);
  const repeatMode = useQueueStore((s) => s.repeatMode);
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

  const { label: statusLabel, tone: statusTone } = getStatusDisplay(status, isPreview);

  const dimColor = theme.color.textTertiary;
  const activeColor = theme.color.accent;

  const RepeatIcon = repeatMode === 'one' ? Repeat1 : Repeat;
  const repeatColor = repeatMode === 'off' ? dimColor : activeColor;

  return (
    <View
      style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}
    >
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <IconButton
            icon={ChevronDown}
            size={28}
            onPress={() => router.back()}
            accessibilityLabel="Close player"
          />
        </View>
        <Text variant="caption" tone={statusTone}>
          {statusLabel}
        </Text>
        <View style={styles.headerActions}>
          <IconButton
            icon={Mic2}
            size={20}
            onPress={() => router.push('/player/lyrics')}
            accessibilityLabel="View lyrics"
          />
          {queueLength > 1 ? (
            <IconButton
              icon={ListMusic}
              size={22}
              onPress={() => router.push('/player/queue')}
              accessibilityLabel="View queue"
            />
          ) : null}
          <IconButton
            icon={MoreHorizontal}
            size={22}
            onPress={() => setOptionsOpen(true)}
            accessibilityLabel="Player options"
          />
        </View>
      </View>

      <View style={styles.artworkContainer}>
        <View style={shadowStyle}>
          <Artwork uri={track.artworkUrl} size={artworkSize} radius={radius.lg} />
        </View>
      </View>

      <View style={styles.info}>
        <Text variant="displayL" numberOfLines={2}>
          {track.title}
        </Text>
        <Text variant="body" tone="secondary" numberOfLines={1}>
          {withFeaturing(track.artist, track.featuredArtists)}
        </Text>
      </View>

      <Scrubber positionMs={positionMs} durationMs={durationMs} onSeek={seekTo} />

      {isError ? (
        <View style={styles.errorControls}>
          <Button label="Retry" onPress={retry} haptic />
        </View>
      ) : isPreview ? (
        <View style={styles.controls}>
          <View style={styles.controlSpacer} />
          <PlayButton isPlaying={isPlaying} isEnded={isEnded} onPress={handlePlayPause} />
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
            color={
              hasPrevious || positionMs > RESTART_THRESHOLD_MS ? theme.color.textPrimary : dimColor
            }
            onPress={handlePrevious}
            accessibilityLabel="Previous track"
          />
          <PlayButton isPlaying={isPlaying} isEnded={isEnded} onPress={handlePlayPause} />
          <IconButton
            icon={SkipForward}
            size={24}
            color={hasNext ? theme.color.textPrimary : dimColor}
            onPress={skipToNext}
            disabled={!hasNext}
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

      <PlayerOptionsSheets open={optionsOpen} onClose={() => setOptionsOpen(false)} />
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
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.lg,
  },
  headerLeft: { flex: 1, alignItems: 'flex-start' },
  headerActions: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    gap: spacing.xs,
  },
  artworkContainer: {
    alignItems: 'center',
    paddingHorizontal: spacing['3xl'],
    paddingBottom: spacing['3xl'],
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
