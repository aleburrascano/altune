import { useCallback, useRef, useState } from 'react';
import { type GestureResponderEvent, type LayoutChangeEvent, PanResponder, StyleSheet, View } from 'react-native';

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
  const durationRef = useRef(durationMs);
  durationRef.current = durationMs;

  const [isDragging, setIsDragging] = useState(false);
  const [dragPosition, setDragPosition] = useState(0);

  const posFromEvent = (evt: GestureResponderEvent): number => {
    const x = evt.nativeEvent.locationX;
    const ratio = Math.max(0, Math.min(1, x / (widthRef.current || 1)));
    return ratio * durationRef.current;
  };

  const panResponder = useRef(
    PanResponder.create({
      onStartShouldSetPanResponder: () => true,
      onMoveShouldSetPanResponder: () => true,
      onPanResponderGrant: (evt) => {
        setIsDragging(true);
        setDragPosition(posFromEvent(evt));
      },
      onPanResponderMove: (evt) => {
        setDragPosition(posFromEvent(evt));
      },
      onPanResponderRelease: (evt) => {
        const finalPos = posFromEvent(evt);
        setDragPosition(finalPos);
        onSeek(finalPos);
        setTimeout(() => setIsDragging(false), 300);
      },
      onPanResponderTerminate: () => {
        setIsDragging(false);
      },
    }),
  ).current;

  const onLayout = useCallback((e: LayoutChangeEvent) => {
    widthRef.current = e.nativeEvent.layout.width;
  }, []);

  const displayPosition = isDragging ? dragPosition : positionMs;
  const displayProgress = durationMs > 0 ? Math.min(1, displayPosition / durationMs) : 0;

  const s = styles(theme);

  return (
    <View style={s.container}>
      <View
        style={s.trackOuter}
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
        <View style={s.trackBg} />
        <View style={[s.trackFill, { width: `${displayProgress * 100}%` }]} />
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
      marginTop: 8,
    },
    trackOuter: {
      height: 40,
      justifyContent: 'center',
    },
    trackBg: {
      position: 'absolute',
      left: 0,
      right: 0,
      height: 4,
      backgroundColor: theme.color.border,
      borderRadius: 2,
    },
    trackFill: {
      position: 'absolute',
      left: 0,
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
      top: 12,
    },
    times: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      marginTop: 4,
    },
  });
