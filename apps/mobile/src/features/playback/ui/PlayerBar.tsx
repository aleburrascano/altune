import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import {
  ListMusic,
  Mic2,
  MoreHorizontal,
  Pause,
  Play,
  RotateCcw,
  SkipForward,
} from 'lucide-react-native';

import { withFeaturing } from '@shared/lib/featured';
import { shouldRestartOnPrevious } from '@shared/playback/constants';
import type { PlaybackTrack } from '@shared/playback/types';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { radius, spacing } from '@shared/ui/theme/tokens';
import { canRetryPlaybackError } from '../retryPolicy';
import { endedLabel, isPreviewTrack } from './playerStatus';
import { PlayerOptionsSheets } from './PlayerOptionsSheets';
import { Scrubber } from './Scrubber';
import { TransportControls } from './TransportControls';
import { usePlaybackTransport } from './usePlaybackTransport';

type Transport = ReturnType<typeof usePlaybackTransport>;
type Theme = ReturnType<typeof useTheme>;

type Flags = { isError: boolean; isEnded: boolean; isPreview: boolean };

function statusLine(
  { isError, isEnded, isPreview }: Flags,
  errorMessage: string | null,
  artistText: string,
) {
  if (isError) return errorMessage ?? 'Playback error';
  if (isEnded) return endedLabel(isPreview);
  return isPreview ? `${artistText} · Preview` : artistText;
}

function barStatus(transport: Transport) {
  const isPreview = isPreviewTrack(transport.track!);
  const artistText = withFeaturing(transport.track!.artist, transport.track!.featuredArtists);
  return statusLine(
    { isError: transport.isError, isEnded: transport.isEnded, isPreview },
    transport.errorMessage,
    artistText,
  );
}

function RetryOrSkip({ hasNext, onSkip }: { hasNext: boolean; onSkip: () => void }) {
  if (!hasNext) return null;
  return <IconButton icon={SkipForward} onPress={onSkip} accessibilityLabel="Skip track" />;
}

type ErrorControlProps = {
  errorKind: Transport['errorKind'];
  hasNext: boolean;
  onRetry: () => void;
  onSkip: () => void;
};

function ErrorControl({ errorKind, hasNext, onRetry, onSkip }: ErrorControlProps) {
  if (canRetryPlaybackError(errorKind))
    return <IconButton icon={RotateCcw} onPress={onRetry} accessibilityLabel="Retry" />;
  return <RetryOrSkip hasNext={hasNext} onSkip={onSkip} />;
}

function EmptyBar({ barStyle }: { barStyle: unknown }) {
  return (
    <View testID="player-bar" style={barStyle as never}>
      <Text variant="label" tone="secondary">
        Pick something to play
      </Text>
    </View>
  );
}

function BarLine({ line }: { line: string }) {
  return (
    <Text variant="caption" tone="secondary" numberOfLines={1}>
      {line}
    </Text>
  );
}

function BarInfo({ title, line }: { title: string; line: string }) {
  return (
    <View style={styles.info}>
      <Text variant="label" numberOfLines={1}>
        {title}
      </Text>
      <BarLine line={line} />
    </View>
  );
}

type BarHeaderProps = { track: PlaybackTrack; line: string; onOpenPlayer: () => void };

function openPlayerLabel(track: PlaybackTrack) {
  return `Open player: ${track.title} by ${track.artist}`;
}

function BarHeaderArt({ track, line }: { track: PlaybackTrack; line: string }) {
  return (
    <>
      <Artwork uri={track.artworkUrl} size={44} radius={radius.sm} />
      <BarInfo title={track.title} line={line} />
    </>
  );
}

function headerPressable(track: PlaybackTrack, onOpenPlayer: () => void) {
  return {
    onPress: onOpenPlayer,
    style: styles.header,
    accessibilityRole: 'button' as const,
    accessibilityLabel: openPlayerLabel(track),
  };
}

function BarHeader({ track, line, onOpenPlayer }: BarHeaderProps) {
  return (
    <Pressable {...headerPressable(track, onOpenPlayer)}>
      <BarHeaderArt track={track} line={line} />
    </Pressable>
  );
}

type PlayPauseProps = { isPlaying: boolean; isEnded: boolean; onPlayPause: () => void };

function PlayPauseControl({ isPlaying, isEnded, onPlayPause }: PlayPauseProps) {
  const icon = isPlaying ? Pause : isEnded ? RotateCcw : Play;
  const label = isPlaying ? 'Pause' : isEnded ? 'Play again' : 'Play';
  return <IconButton icon={icon} size={22} onPress={onPlayPause} accessibilityLabel={label} />;
}

function transportFlags(transport: Transport) {
  return {
    shuffled: transport.shuffled,
    repeatMode: transport.repeatMode,
    hasNext: transport.hasNext,
    hasPrevious: transport.hasPrevious,
    canRestart: shouldRestartOnPrevious(transport.positionMs),
  };
}

function transportHandlers(transport: Transport) {
  return {
    onToggleShuffle: transport.toggleShuffle,
    onPrevious: transport.onPrevious,
    onNext: transport.skipToNext,
    onCycleRepeat: transport.cycleRepeatMode,
  };
}

function transportColors(theme: Theme) {
  return {
    dimColor: theme.color.textTertiary,
    activeColor: theme.color.accent,
    primaryColor: theme.color.textPrimary,
  };
}

function BarTransport({ transport, theme }: { transport: Transport; theme: Theme }) {
  const center = (
    <PlayPauseControl
      isPlaying={transport.isPlaying}
      isEnded={transport.isEnded}
      onPlayPause={transport.onPlayPause}
    />
  );
  return (
    <TransportControls
      {...transportFlags(transport)}
      {...transportHandlers(transport)}
      {...transportColors(theme)}
      smallIconSize={18}
      largeIconSize={20}
      center={center}
    />
  );
}

function BarMiddle({ transport, theme }: { transport: Transport; theme: Theme }) {
  if (transport.isError) {
    return (
      <ErrorControl
        errorKind={transport.errorKind}
        hasNext={transport.hasNext}
        onRetry={transport.retry}
        onSkip={transport.skipToNext}
      />
    );
  }
  return (
    <View style={styles.transport}>
      <BarTransport transport={transport} theme={theme} />
    </View>
  );
}

type ActionsProps = { onLyrics: () => void; onQueue: () => void; onOptions: () => void };

function BarActions({ onLyrics, onQueue, onOptions }: ActionsProps) {
  return (
    <>
      <IconButton icon={Mic2} size={20} onPress={onLyrics} accessibilityLabel="View lyrics" />
      <IconButton icon={ListMusic} size={20} onPress={onQueue} accessibilityLabel="View queue" />
      <IconButton
        icon={MoreHorizontal}
        size={20}
        onPress={onOptions}
        accessibilityLabel="Player options"
      />
    </>
  );
}

type FooterProps = {
  transport: Transport;
  router: ReturnType<typeof useRouter>;
  optionsOpen: boolean;
  setOptionsOpen: (open: boolean) => void;
};

function BarFooter({ transport, router, optionsOpen, setOptionsOpen }: FooterProps) {
  return (
    <>
      <View style={styles.scrubber}>
        <Scrubber
          positionMs={transport.positionMs}
          durationMs={transport.durationMs}
          onSeek={transport.seekTo}
        />
      </View>
      <BarActions
        onLyrics={() => router.push('/player/lyrics')}
        onQueue={() => router.push('/player/queue')}
        onOptions={() => setOptionsOpen(true)}
      />
      <PlayerOptionsSheets open={optionsOpen} onClose={() => setOptionsOpen(false)} />
    </>
  );
}

function useBarStyle() {
  const theme = useTheme();
  return [
    styles.bar,
    { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border },
  ];
}

function usePlayerBarState() {
  const transport = usePlaybackTransport();
  const [optionsOpen, setOptionsOpen] = useState(false);
  const theme = useTheme();
  const router = useRouter();
  const barStyle = useBarStyle();
  return { transport, optionsOpen, setOptionsOpen, theme, router, barStyle };
}

function openPlayer(router: ReturnType<typeof useRouter>) {
  return () => router.push('/player');
}

function BarShell({ state }: { state: ReturnType<typeof usePlayerBarState> }) {
  const { transport, theme, router, optionsOpen, setOptionsOpen, barStyle } = state;
  return (
    <View testID="player-bar" style={barStyle}>
      <BarHeader
        track={transport.track!}
        line={barStatus(transport)}
        onOpenPlayer={openPlayer(router)}
      />
      <BarMiddle transport={transport} theme={theme} />
      <BarFooter
        transport={transport}
        router={router}
        optionsOpen={optionsOpen}
        setOptionsOpen={setOptionsOpen}
      />
    </View>
  );
}

export function PlayerBar() {
  const state = usePlayerBarState();
  if (!state.transport.track) return <EmptyBar barStyle={state.barStyle} />;
  return <BarShell state={state} />;
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    gap: spacing.md,
    minHeight: 64,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
  },
  info: {
    width: 160,
    gap: 2,
  },
  transport: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
  },
  scrubber: {
    flex: 1,
  },
});
