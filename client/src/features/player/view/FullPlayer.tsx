import { useMemo, useState } from 'react';
import { StyleSheet, useWindowDimensions, View } from 'react-native';
import { ListMusic, Mic2, MoreHorizontal, Pause, Play, RotateCcw } from 'lucide-react-native';
import { withFeaturing } from '@shared/catalog';
import { usePlayerNavigation } from '@features/player/view/PlayerNavigationProvider';
import { shouldRestartOnPrevious } from '@features/player/core/restartOnPrevious';
import { useQueueStore } from '@shared/playback';
import type { PlaybackErrorKind, PlaybackStatus } from '@shared/playback';
import {
  canRetryPlaybackError,
  endedLabel,
  isPreviewTrack,
  libraryTrackId,
} from '@features/player/core/playerStatus';
import { PlayerOptionsSheets } from '@features/player/view/PlayerOptionsSheets';
import { Scrubber } from '@features/player/view/Scrubber';
import { VolumeSlider } from '@features/player/view/web/VolumeSlider';
import {
  SheetHeader,
  SheetHeaderCenter,
  SheetHeaderTrailing,
  SheetScreen,
} from '@features/player/view/SheetHeader';
import { TransportControls } from '@features/player/view/TransportControls';
import { usePlaybackTransport } from '@features/player/view/usePlaybackTransport';
import {
  Artwork,
  Text,
  Button,
  IconButton,
  useTheme,
  halved,
  touchTarget,
  radius,
  spacing,
} from '@shared/ui';

function getStatusDisplay(
  status: PlaybackStatus,
  isPreview: boolean,
): { label: string; tone: 'danger' | 'warning' | 'secondary' } {
  if (status === 'error') return { label: 'Error', tone: 'danger' };
  if (status === 'ended') {
    return { label: endedLabel(isPreview), tone: 'warning' };
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
  trackId,
}: {
  isPlaying: boolean;
  isEnded: boolean;
  onPress: () => void;
  trackId?: string | undefined;
}) {
  const theme = useTheme();
  return (
    <View style={[styles.playButton, { backgroundColor: theme.color.accent }]}>
      <IconButton
        action="playback.full.play_pause"
        trackId={trackId}
        icon={isPlaying ? Pause : isEnded ? RotateCcw : Play}
        size={32}
        color={theme.color.onAccent}
        onPress={onPress}
        accessibilityLabel={isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play'}
      />
    </View>
  );
}

function ErrorAction({
  errorKind,
  hasNext,
  onRetry,
  onSkip,
}: {
  errorKind: PlaybackErrorKind | null;
  hasNext: boolean;
  onRetry: () => void;
  onSkip: () => void;
}) {
  if (canRetryPlaybackError(errorKind))
    return <Button action="playback.full.retry" label="Retry" onPress={onRetry} haptic />;
  if (!hasNext) return null;
  return <Button action="playback.full.skip" label="Skip track" onPress={onSkip} haptic />;
}

export function FullPlayer() {
  const {
    status,
    track,
    positionMs,
    durationMs,
    seekTo,
    retry,
    errorKind,
    skipToNext,
    toggleShuffle,
    cycleRepeatMode,
    shuffled,
    repeatMode,
    hasNext,
    hasPrevious,
    isPlaying,
    isEnded,
    isError,
    onPrevious,
    onPlayPause,
  } = usePlaybackTransport();
  const [optionsOpen, setOptionsOpen] = useState(false);
  const queueLength = useQueueStore((s) => s.playOrder.length);
  const theme = useTheme();
  const navigation = usePlayerNavigation();
  const { width: screenWidth } = useWindowDimensions();
  const artworkSize = screenWidth - spacing['3xl'] * 2;
  const shadowStyle = useMemo(
    () => ({ boxShadow: `0 8px 24px ${theme.color.accent}59` }),
    [theme.color.accent],
  );

  if (!track) {
    return null;
  }

  const isPreview = isPreviewTrack(track);

  const { label: statusLabel, tone: statusTone } = getStatusDisplay(status, isPreview);

  const dimColor = theme.color.textTertiary;
  const activeColor = theme.color.accent;

  return (
    <SheetScreen>
      <SheetHeader onClose={() => navigation.close()} closeLabel="Close player">
        <SheetHeaderCenter>
          <Text variant="caption" tone={statusTone}>
            {statusLabel}
          </Text>
        </SheetHeaderCenter>
        <SheetHeaderTrailing>
          <IconButton
            action="playback.full.lyrics"
            icon={Mic2}
            size={20}
            onPress={() => navigation.openLyrics()}
            accessibilityLabel="View lyrics"
          />
          {queueLength > 1 ? (
            <IconButton
              action="playback.full.queue"
              icon={ListMusic}
              size={22}
              onPress={() => navigation.openQueue()}
              accessibilityLabel="View queue"
            />
          ) : null}
          <IconButton
            action="playback.full.options"
            icon={MoreHorizontal}
            size={22}
            onPress={() => setOptionsOpen(true)}
            accessibilityLabel="Player options"
          />
        </SheetHeaderTrailing>
      </SheetHeader>

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

      <VolumeSlider />

      {isError ? (
        <View style={styles.errorControls}>
          <ErrorAction
            errorKind={errorKind}
            hasNext={hasNext}
            onRetry={retry}
            onSkip={skipToNext}
          />
        </View>
      ) : isPreview ? (
        <View style={styles.controls}>
          <View style={styles.controlSpacer} />
          <PlayButton
            isPlaying={isPlaying}
            isEnded={isEnded}
            onPress={onPlayPause}
            trackId={libraryTrackId(track)}
          />
          <View style={styles.controlSpacer} />
        </View>
      ) : (
        <View style={styles.controls}>
          <TransportControls
            shuffled={shuffled}
            repeatMode={repeatMode}
            hasNext={hasNext}
            hasPrevious={hasPrevious}
            canRestart={shouldRestartOnPrevious(positionMs)}
            smallIconSize={20}
            largeIconSize={24}
            dimColor={dimColor}
            activeColor={activeColor}
            primaryColor={theme.color.textPrimary}
            onToggleShuffle={toggleShuffle}
            onPrevious={onPrevious}
            onNext={skipToNext}
            onCycleRepeat={cycleRepeatMode}
            center={
              <PlayButton
                isPlaying={isPlaying}
                isEnded={isEnded}
                onPress={onPlayPause}
                trackId={libraryTrackId(track)}
              />
            }
          />
        </View>
      )}

      <PlayerOptionsSheets open={optionsOpen} onClose={() => setOptionsOpen(false)} />
    </SheetScreen>
  );
}

const PLAY_BUTTON_SIZE = 64;

const styles = StyleSheet.create({
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
    width: touchTarget,
  },
  errorControls: {
    alignItems: 'center',
    paddingTop: spacing['2xl'],
  },
  playButton: {
    width: PLAY_BUTTON_SIZE,
    height: PLAY_BUTTON_SIZE,
    borderRadius: halved(PLAY_BUTTON_SIZE),
    alignItems: 'center',
    justifyContent: 'center',
  },
});
