import type { ReactElement } from 'react';
import { Animated, StyleSheet, View } from 'react-native';
import { GestureDetector } from 'react-native-gesture-handler';

import { Text } from '@shared/ui/primitives/Text';
import { useWideWebLayout } from '@shared/ui/layout/useLayoutMode';
import { useTheme, halved, touchTarget } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

import { useScrubberGesture } from '@features/playback/hooks/useScrubberGesture';
import { msToSeconds } from '@features/playback/domain/time';
import { usePlaybackVolumeStore } from '@features/playback/web/playbackVolume';

interface ScrubberProps {
  positionMs: number;
  durationMs: number;
  onSeek: (positionMs: number) => void;
}

function formatMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0:00';
  const totalSeconds = Math.floor(msToSeconds(ms));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, '0')}`;
}

export function Scrubber({ positionMs, durationMs, onSeek }: ScrubberProps) {
  const theme = useTheme();
  const {
    trackRef,
    panGesture,
    isDragging,
    labelMs,
    onLayout,
    onAccessibilityAction,
    fillWidth,
    thumbLeft,
  } = useScrubberGesture({ positionMs, durationMs, onSeek });

  return (
    <View style={styles.container}>
      <GestureDetector gesture={panGesture}>
        <View
          ref={trackRef}
          style={styles.trackOuter}
          onLayout={onLayout}
          accessibilityRole="adjustable"
          accessibilityLabel={`Playback position: ${formatMs(labelMs)} of ${formatMs(durationMs)}`}
          accessibilityValue={{
            min: 0,
            max: 100,
            now: Math.round(durationMs > 0 ? (labelMs / durationMs) * 100 : 0),
          }}
          accessibilityActions={[{ name: 'increment' }, { name: 'decrement' }]}
          onAccessibilityAction={onAccessibilityAction}
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
      </GestureDetector>
      <View style={styles.times}>
        <Text variant="caption" tone="secondary">
          {formatMs(durationMs > 0 ? labelMs : positionMs)}
        </Text>
        <Text variant="caption" tone="secondary">
          {durationMs > 0 ? `-${formatMs(Math.max(0, durationMs - labelMs))}` : ''}
        </Text>
      </View>
    </View>
  );
}

const TRACK_THICKNESS = 3;
const THUMB_SIZE = 14;

const styles = StyleSheet.create({
  container: {
    width: '100%',
    paddingHorizontal: spacing['2xl'],
  },
  trackOuter: {
    height: touchTarget,
    justifyContent: 'center',
  },
  trackBg: {
    position: 'absolute',
    left: 0,
    right: 0,
    height: TRACK_THICKNESS,
    borderRadius: halved(TRACK_THICKNESS),
  },
  trackFill: {
    position: 'absolute',
    left: 0,
    height: TRACK_THICKNESS,
    borderRadius: halved(TRACK_THICKNESS),
  },
  thumb: {
    position: 'absolute',
    width: THUMB_SIZE,
    height: THUMB_SIZE,
    borderRadius: halved(THUMB_SIZE),
    marginLeft: -halved(THUMB_SIZE),
    top: halved(touchTarget - THUMB_SIZE),
  },
  times: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    marginTop: spacing.xs,
  },
});

export function VolumeSlider(): ReactElement | null {
  const theme = useTheme();
  const isWide = useWideWebLayout();
  const volume = usePlaybackVolumeStore((s) => s.volume);
  const setVolume = usePlaybackVolumeStore((s) => s.setVolume);
  if (!isWide) return null;
  return (
    <input
      type="range"
      aria-label="Volume"
      min={0}
      max={1}
      step={0.01}
      value={volume}
      onChange={(event) => setVolume(Number(event.currentTarget.value))}
      style={{ width: 120, accentColor: theme.color.accent }}
    />
  );
}
