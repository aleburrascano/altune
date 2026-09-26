import { memo, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import type { StyleProp, TextStyle } from 'react-native';

import { formatDuration } from '@shared/lib/format';
import { Text, useIsWideWebLayout, useTheme } from '@shared/ui';
import type { Theme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import { usePinnedStatus } from '../hooks/useLibraryOffline';
import { LibraryRowDetails } from './LibraryRowDetails';
import { LibraryRowFailure } from './LibraryRowFailure';
import { LibraryRowMoreButton } from './LibraryRowMoreButton';
import { LibraryRowPinnedIcon } from './LibraryRowPinnedIcon';
import { LibraryRowPlayback } from './LibraryRowPlayback';
import { LibraryRowSelection } from './LibraryRowSelection';
import { libraryRowAccessibilityLabel } from './libraryRowLabels';
import { libraryRowWideStyle } from './libraryRowWideStyle';
import { WIDE_TRACK_COLUMNS } from './WideTrackHeader';

import type { TrackResponse } from '@shared/api-client/types';

type WideRowProps = {
  track: TrackResponse;
  a11yLabel: string;
  pinned: ReturnType<typeof usePinnedStatus>;
  onPlay?: (() => void) | undefined;
  onPress: () => void;
  onMore: (anchor: MenuAnchor) => void;
  onRetry?: (() => void) | undefined;
  retrying: boolean;
  isPlaying: boolean;
  onLongPress?: (() => void) | undefined;
};

function wideDuration(track: TrackResponse): string {
  if (track.duration_seconds == null || track.duration_seconds <= 0) return '';
  return formatDuration(track.duration_seconds);
}

function playTrackHandler(track: TrackResponse, onPlay: (() => void) | undefined, onPress: () => void) {
  return () => {
    if (track.acquisition_status === 'ready' && onPlay) onPlay();
    else onPress();
  };
}

function WideTitle({ text, playing }: { text: string; playing: boolean }): ReactElement {
  return (
    <Text variant="bodyStrong" tone={playing ? 'accent' : 'primary'} numberOfLines={1} style={wideStyles.title}>
      {text}
    </Text>
  );
}

function WideLabel({ text, style }: { text: string; style: StyleProp<TextStyle> }): ReactElement {
  return (
    <Text variant="label" tone="secondary" numberOfLines={1} style={style}>
      {text}
    </Text>
  );
}

function WideDuration({ track }: { track: TrackResponse }): ReactElement {
  return (
    <Text variant="caption" tone="tertiary" style={wideStyles.duration}>
      {wideDuration(track)}
    </Text>
  );
}

function WideRowIdentity({ track, isPlaying }: { track: TrackResponse; isPlaying: boolean }): ReactElement {
  return (
    <>
      <Artwork uri={track.artwork_url} size={48} radius={6} accessibilityLabel="Album art" />
      <WideTitle text={track.title} playing={isPlaying} />
      <WideLabel text={track.artist} style={wideStyles.artist} />
      <WideLabel text={track.album ?? ''} style={wideStyles.album} />
    </>
  );
}

function WideRowFailure(props: { track: TrackResponse; retrying: boolean; onRetry: (() => void) | undefined }): ReactElement | null {
  if (props.track.acquisition_status !== 'failed') return null;
  return <LibraryRowFailure track={props.track} retrying={props.retrying} onRetry={props.onRetry} />;
}

type WideStatusProps = {
  track: TrackResponse;
  pinned: ReturnType<typeof usePinnedStatus>;
  retrying: boolean;
  onRetry?: (() => void) | undefined;
  onMore: (anchor: MenuAnchor) => void;
};

function WideRowStatus({ track, pinned, retrying, onRetry, onMore }: WideStatusProps): ReactElement {
  return (
    <View style={wideStyles.status}>
      <LibraryRowPinnedIcon trackId={track.id} status={pinned} />
      <WideRowFailure track={track} retrying={retrying} onRetry={onRetry} />
      <LibraryRowMoreButton trackId={track.id} title={track.title} onMore={onMore} />
    </View>
  );
}

function wideRowPressableProps(props: WideRowProps, theme: Theme) {
  return {
    testID: `library-row-${props.track.id}`,
    onPress: playTrackHandler(props.track, props.onPlay, props.onPress),
    ...(props.onLongPress != null ? { onLongPress: props.onLongPress } : {}),
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.a11yLabel,
    style: libraryRowWideStyle(theme, props.isPlaying),
  };
}

function LibraryRowWide(props: WideRowProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...wideRowPressableProps(props, theme)}>
      <WideRowIdentity track={props.track} isPlaying={props.isPlaying} />
      <WideDuration track={props.track} />
      <WideRowStatus {...props} />
    </Pressable>
  );
}

const wideStyles = StyleSheet.create({
  title: { flex: 3 },
  artist: { flex: 2 },
  album: { flex: 2 },
  duration: { width: WIDE_TRACK_COLUMNS.duration, textAlign: 'right' },
  status: {
    width: WIDE_TRACK_COLUMNS.status,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
  },
});

type LibraryRowProps = {
  track: TrackResponse;
  onPlay?: () => void;
  onPress: () => void;
  onMore: (anchor: MenuAnchor) => void;
  onRetry?: (() => void) | undefined;
  retrying?: boolean;
  isPlaying?: boolean;
  selectable?: { selected: boolean; onToggle: () => void } | undefined;
  onLongPress?: (() => void) | undefined;
};

function LibraryRowImpl({
  track,
  onPlay,
  onPress,
  onMore,
  onRetry,
  retrying,
  isPlaying,
  selectable,
  onLongPress,
}: LibraryRowProps): ReactElement {
  const pinned = usePinnedStatus(track.id);
  const isRetrying = retrying === true;
  const isWide = useIsWideWebLayout();
  const a11yLabel = libraryRowAccessibilityLabel({
    track,
    retrying: isRetrying,
    canRetry: onRetry != null,
    pinned,
  });
  const details = (
    <LibraryRowDetails
      track={track}
      isPlaying={isPlaying === true}
      retrying={isRetrying}
      onRetry={onRetry}
    />
  );

  if (selectable == null && isWide) {
    return (
      <LibraryRowWide
        track={track}
        a11yLabel={a11yLabel}
        pinned={pinned}
        onPlay={onPlay}
        onPress={onPress}
        onMore={onMore}
        onRetry={onRetry}
        retrying={isRetrying}
        isPlaying={isPlaying === true}
        onLongPress={onLongPress}
      />
    );
  }

  if (selectable != null) {
    return (
      <LibraryRowSelection
        track={track}
        a11yLabel={a11yLabel}
        selected={selectable.selected}
        onToggle={selectable.onToggle}
        onLongPress={onLongPress}
      >
        {details}
      </LibraryRowSelection>
    );
  }

  return (
    <LibraryRowPlayback
      track={track}
      a11yLabel={a11yLabel}
      pinned={pinned}
      onPlay={onPlay}
      onPress={onPress}
      onMore={onMore}
      onLongPress={onLongPress}
    >
      {details}
    </LibraryRowPlayback>
  );
}

export const LibraryRow = memo(LibraryRowImpl, (prev, next) => {
  const a = prev.track;
  const b = next.track;
  return (
    a.id === b.id &&
    a.title === b.title &&
    a.artist === b.artist &&
    a.album === b.album &&
    a.artwork_url === b.artwork_url &&
    a.duration_seconds === b.duration_seconds &&
    a.acquisition_status === b.acquisition_status &&
    a.failure_reason === b.failure_reason &&
    a.failure_message === b.failure_message &&
    prev.retrying === next.retrying &&
    prev.isPlaying === next.isPlaying &&
    prev.selectable?.selected === next.selectable?.selected &&
    (prev.selectable == null) === (next.selectable == null) &&
    (prev.onLongPress == null) === (next.onLongPress == null) &&
    (prev.onPlay == null) === (next.onPlay == null) &&
    (prev.onRetry == null) === (next.onRetry == null)
  );
});
