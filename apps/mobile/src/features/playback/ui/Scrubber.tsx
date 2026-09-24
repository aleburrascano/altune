import { Animated, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import { useScrubberGesture } from './useScrubberGesture';

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
  const {
    trackRef,
    panHandlers,
    isDragging,
    labelMs,
    onLayout,
    onAccessibilityAction,
    fillWidth,
    thumbLeft,
  } = useScrubberGesture({ positionMs, durationMs, onSeek });

  return (
    <View style={styles.container}>
      <View
        ref={trackRef}
        style={styles.trackOuter}
        onLayout={onLayout}
        accessibilityRole="adjustable"
        accessibilityLabel={`Playback position: ${formatTime(labelMs)} of ${formatTime(durationMs)}`}
        accessibilityValue={{
          min: 0,
          max: 100,
          now: Math.round(durationMs > 0 ? (labelMs / durationMs) * 100 : 0),
        }}
        accessibilityActions={[{ name: 'increment' }, { name: 'decrement' }]}
        onAccessibilityAction={onAccessibilityAction}
        {...panHandlers}
      >
        <View style={[styles.trackBg, { backgroundColor: theme.color.border }]} />
        <Animated.View
          style={[styles.trackFill, { width: fillWidth, backgroundColor: theme.color.accent }]}
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
