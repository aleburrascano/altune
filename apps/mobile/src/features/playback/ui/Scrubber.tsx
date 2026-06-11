import { useCallback, useRef, useState } from 'react';
import { type LayoutChangeEvent, PanResponder, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import type { Theme } from '@shared/ui/theme';

interface ScrubberProps {
  positionMs: number;
  durationMs: number;
  onSeek: (positionMs: number) => void;
}

function formatTime(ms: number): string {
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export function Scrubber({ positionMs, durationMs, onSeek }: ScrubberProps) {
  const theme = useTheme();
  const widthRef = useRef(0);
  const [isDragging, setIsDragging] = useState(false);
  const [dragPosition, setDragPosition] = useState(0);

  const progress = durationMs > 0 ? positionMs / durationMs : 0;
  const displayPosition = isDragging ? dragPosition : positionMs;
  const displayProgress = durationMs > 0 ? displayPosition / durationMs : 0;

  const panResponder = useRef(
    PanResponder.create({
      onStartShouldSetPanResponder: () => true,
      onMoveShouldSetPanResponder: () => true,
      onPanResponderGrant: (evt) => {
        setIsDragging(true);
        const x = evt.nativeEvent.locationX;
        const pos = Math.max(0, Math.min(1, x / widthRef.current)) * durationMs;
        setDragPosition(pos);
      },
      onPanResponderMove: (evt) => {
        const x = evt.nativeEvent.locationX;
        const pos = Math.max(0, Math.min(1, x / widthRef.current)) * durationMs;
        setDragPosition(pos);
      },
      onPanResponderRelease: () => {
        setIsDragging(false);
        onSeek(dragPosition);
      },
    }),
  ).current;

  const onLayout = useCallback((e: LayoutChangeEvent) => {
    widthRef.current = e.nativeEvent.layout.width;
  }, []);

  const s = styles(theme);

  return (
    <View style={s.container}>
      <View
        style={s.track}
        onLayout={onLayout}
        accessibilityRole="adjustable"
        accessibilityLabel={`Playback position: ${formatTime(displayPosition)} of ${formatTime(durationMs)}`}
        accessibilityValue={{
          min: 0,
          max: 100,
          now: Math.round(displayProgress * 100),
        }}
        {...panResponder.panHandlers}
      >
        <View style={[s.fill, { width: `${displayProgress * 100}%` }]} />
        <View style={[s.thumb, { left: `${displayProgress * 100}%` }]} />
      </View>
      <View style={s.times}>
        <Text variant="bodySmall">{formatTime(displayPosition)}</Text>
        <Text variant="bodySmall">-{formatTime(Math.max(0, durationMs - displayPosition))}</Text>
      </View>
    </View>
  );
}

const styles = (theme: Theme) =>
  StyleSheet.create({
    container: {
      width: '100%',
      paddingHorizontal: 24,
    },
    track: {
      height: 32,
      justifyContent: 'center',
    },
    fill: {
      height: 4,
      backgroundColor: theme.color.accent,
      borderRadius: 2,
    },
    thumb: {
      position: 'absolute',
      width: 16,
      height: 16,
      borderRadius: 8,
      backgroundColor: theme.color.accent,
      marginLeft: -8,
      top: 8,
    },
    times: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      marginTop: 4,
    },
  });
