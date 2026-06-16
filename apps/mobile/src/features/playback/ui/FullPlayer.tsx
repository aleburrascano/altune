import { useMemo } from 'react';
import { StyleSheet, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { ChevronDown, Pause, Play, RotateCcw, SkipBack, SkipForward } from 'lucide-react-native';

import { usePlayback } from '../hooks/usePlayback';
import { Scrubber } from './Scrubber';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { Button } from '@shared/ui/primitives/Button';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';

const SKIP_MS = 10_000;

export function FullPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, seekTo, retry } = usePlayback();
  const theme = useTheme();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width: screenWidth } = useWindowDimensions();
  const artworkSize = screenWidth - spacing['3xl'] * 2;

  if (!track) {
    return null;
  }

  const isPlaying = status === 'playing';
  const shadowStyle = useMemo(
    () => ({ boxShadow: `0 8px 24px ${theme.color.accent}59` }),
    [theme.color.accent],
  );
  const isPreview = track.source.kind === 'preview';
  const isError = status === 'error';
  const isEnded = status === 'ended';

  const skipBack = () => seekTo(Math.max(0, positionMs - SKIP_MS));
  const skipForward = () => seekTo(Math.min(durationMs, positionMs + SKIP_MS));

  const statusLabel = isError
    ? 'Error'
    : isEnded
      ? isPreview ? 'Preview ended' : 'Finished'
      : isPreview ? 'Preview' : 'Now Playing';

  const statusTone = isError ? 'danger' : isPreview || isEnded ? 'warning' : 'secondary';

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
        <View style={styles.headerSpacer} />
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
      ) : (
        <View style={styles.controls}>
          <IconButton
            icon={SkipBack}
            size={24}
            onPress={skipBack}
            accessibilityLabel="Skip back 10 seconds"
          />
          <View style={[styles.playButton, { backgroundColor: theme.color.accent }]}>
            <IconButton
              icon={isPlaying ? Pause : isEnded ? RotateCcw : Play}
              size={32}
              color={theme.color.onAccent}
              onPress={isPlaying ? pause : isEnded ? () => { seekTo(0); resume(); } : resume}
              accessibilityLabel={isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play'}
            />
          </View>
          <IconButton
            icon={SkipForward}
            size={24}
            onPress={skipForward}
            accessibilityLabel="Skip forward 10 seconds"
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
    gap: spacing['2xl'],
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
