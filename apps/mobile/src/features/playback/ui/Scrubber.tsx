import { useCallback, useEffect, useRef, useState } from 'react';
import { Animated, type LayoutChangeEvent, PanResponder, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

interface ScrubberProps {
  positionMs: number;
  durationMs: number;
  onSeek: (positionMs: number) => void;
}

function formatTime(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0:00';
  const totalSeconds = Math.floor(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export function Scrubber({ positionMs, durationMs, onSeek }: ScrubberProps) {
  const theme = useTheme();
  const trackRef = useRef<View>(null);
  const layoutRef = useRef({ pageX: 0, width: 0 });

  const durationRef = useRef(durationMs);
  durationRef.current = durationMs;
  const onSeekRef = useRef(onSeek);
  onSeekRef.current = onSeek;
  const positionRef = useRef(positionMs);
  positionRef.current = positionMs;

  const progressRef = useRef<Animated.Value | null>(null);
  if (progressRef.current === null) progressRef.current = new Animated.Value(0);
  const progress = progressRef.current;
  const isDraggingRef = useRef(false);
  const isHoldingSeek = useRef(false);
  const seekHoldTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastLabelUpdate = useRef(0);

  const [isDragging, setIsDragging] = useState(false);
  const [labelMs, setLabelMs] = useState(positionMs);

  useEffect(() => {
    if (!isDraggingRef.current && !isHoldingSeek.current && durationMs > 0) {
      progress.setValue(positionMs / durationMs);
      setLabelMs(positionMs);
    }
  }, [positionMs, durationMs, progress]);

  const ratioFromPageX = (pageX: number): number => {
    const x = pageX - layoutRef.current.pageX;
    return Math.max(0, Math.min(1, x / (layoutRef.current.width || 1)));
  };

  const panResponderRef = useRef<ReturnType<typeof PanResponder.create> | null>(null);
  if (panResponderRef.current === null) {
    panResponderRef.current = PanResponder.create({
      onStartShouldSetPanResponder: () => true,
      onMoveShouldSetPanResponder: () => true,
      onPanResponderTerminationRequest: () => false,
      onPanResponderGrant: (evt) => {
        isDraggingRef.current = true;
        setIsDragging(true);
        if (seekHoldTimer.current) {
          clearTimeout(seekHoldTimer.current);
          seekHoldTimer.current = null;
        }
        isHoldingSeek.current = false;
        const ratio = ratioFromPageX(evt.nativeEvent.pageX);
        progress.setValue(ratio);
        setLabelMs(ratio * durationRef.current);
        lastLabelUpdate.current = Date.now();
      },
      onPanResponderMove: (evt) => {
        const ratio = ratioFromPageX(evt.nativeEvent.pageX);
        progress.setValue(ratio);
        const now = Date.now();
        if (now - lastLabelUpdate.current > 80) {
          lastLabelUpdate.current = now;
          setLabelMs(ratio * durationRef.current);
        }
      },
      onPanResponderRelease: (evt) => {
        const ratio = ratioFromPageX(evt.nativeEvent.pageX);
        const ms = ratio * durationRef.current;
        progress.setValue(ratio);
        setLabelMs(ms);
        isDraggingRef.current = false;
        setIsDragging(false);
        onSeekRef.current(ms);
        isHoldingSeek.current = true;
        seekHoldTimer.current = setTimeout(() => {
          isHoldingSeek.current = false;
          if (durationRef.current > 0) {
            progress.setValue(positionRef.current / durationRef.current);
            setLabelMs(positionRef.current);
          }
        }, 600);
      },
      onPanResponderTerminate: () => {
        isDraggingRef.current = false;
        setIsDragging(false);
        isHoldingSeek.current = false;
        if (seekHoldTimer.current) {
          clearTimeout(seekHoldTimer.current);
          seekHoldTimer.current = null;
        }
      },
    });
  }
  const panResponder = panResponderRef.current;

  const remeasure = useCallback(() => {
    trackRef.current?.measureInWindow((x, _y, width) => {
      if (width > 0) layoutRef.current = { pageX: x, width };
    });
  }, []);

  const onLayout = useCallback(
    (_e: LayoutChangeEvent) => { remeasure(); },
    [remeasure],
  );

  const fillWidth = progress.interpolate({
    inputRange: [0, 1],
    outputRange: ['0%', '100%'],
  });
  const thumbLeft = progress.interpolate({
    inputRange: [0, 1],
    outputRange: ['0%', '100%'],
  });

  return (
    <View style={styles.container}>
      <View
        ref={trackRef}
        style={styles.trackOuter}
        onLayout={onLayout}
        accessibilityRole="adjustable"
        accessibilityLabel={`Playback position: ${formatTime(labelMs)} of ${formatTime(durationMs)}`}
        accessibilityValue={{ min: 0, max: 100, now: Math.round(durationMs > 0 ? (labelMs / durationMs) * 100 : 0) }}
        {...panResponder.panHandlers}
      >
        <View style={[styles.trackBg, { backgroundColor: theme.color.border }]} />
        <Animated.View
          style={[
            styles.trackFill,
            { width: fillWidth, backgroundColor: theme.color.accent },
          ]}
        />
        <Animated.View
          style={[
            styles.thumb,
            {
              left: thumbLeft,
              backgroundColor: theme.color.accent,
              transform: [{ scale: isDragging ? 1.3 : 1 }],
            },
          ]}
        />
      </View>
      <View style={styles.times}>
        <Text variant="caption" tone="secondary">
          {formatTime(durationMs > 0 ? labelMs : positionMs)}
        </Text>
        <Text variant="caption" tone="secondary">
          {durationMs > 0 ? `-${formatTime(Math.max(0, durationMs - labelMs))}` : ''}
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    width: '100%',
    paddingHorizontal: spacing['2xl'],
  },
  trackOuter: {
    height: 44,
    justifyContent: 'center',
  },
  trackBg: {
    position: 'absolute',
    left: 0,
    right: 0,
    height: 3,
    borderRadius: 1.5,
  },
  trackFill: {
    position: 'absolute',
    left: 0,
    height: 3,
    borderRadius: 1.5,
  },
  thumb: {
    position: 'absolute',
    width: 14,
    height: 14,
    borderRadius: 7,
    marginLeft: -7,
    top: 15,
  },
  times: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginTop: spacing.xs,
  },
});
