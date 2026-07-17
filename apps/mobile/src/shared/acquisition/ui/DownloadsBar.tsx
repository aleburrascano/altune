/**
 * DownloadsBar — the in-flight acquisition strip of the Activity Dock.
 *
 * Artwork + a two-line hierarchy (heading over live phase) + a full-width
 * 3-segment progress that fills by phase, with a gentle slide-in on appear and
 * a soft pulse on the active segment. RN `Animated` only (Expo Go safe). The
 * parent mounts it only when something is downloading, so it always has items.
 */

import type { ReactElement } from 'react';
import { useEffect, useRef } from 'react';
import { Animated, Pressable, StyleSheet, View } from 'react-native';

import { ChevronUp } from 'lucide-react-native';

import { aggregatePhase, type DownloadEntry } from '@shared/acquisition/downloadStore';
import { ACQUISITION_PHASES, phaseLabel } from '@shared/acquisition/stagePhase';
import { Text, spacing, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { radius } from '@shared/ui/theme/tokens';

interface DownloadsBarProps {
  items: DownloadEntry[];
  onPress: () => void;
}

export function DownloadsBar({ items, onPress }: DownloadsBarProps): ReactElement | null {
  const theme = useTheme();
  const first = items[0];

  const enter = useRef(new Animated.Value(0)).current;
  const pulse = useRef(new Animated.Value(0.5)).current;
  useEffect(() => {
    Animated.timing(enter, { toValue: 1, duration: 240, useNativeDriver: true }).start();
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(pulse, { toValue: 1, duration: 720, useNativeDriver: true }),
        Animated.timing(pulse, { toValue: 0.45, duration: 720, useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [enter, pulse]);

  if (first == null) return null;

  // Aggregate phase across the whole batch (F9): the least-advanced active item,
  // so the bar reflects the earliest work still happening — not just items[0].
  const phase = aggregatePhase(items) ?? 'finding';
  // 'done' fills every segment (indexOf returns -1); the 3 progress phases map
  // to their segment index.
  const activeIndex = phase === 'done' ? ACQUISITION_PHASES.length : ACQUISITION_PHASES.indexOf(phase);
  const active = items.filter((i) => i.phase !== 'done').length;
  const count = active > 0 ? active : items.length;
  const heading =
    phase === 'done'
      ? 'Done'
      : count === 1
        ? `Downloading "${first.title ?? 'track'}"`
        : `Downloading ${count} songs`;

  return (
    <Animated.View
      style={{
        opacity: enter,
        transform: [{ translateY: enter.interpolate({ inputRange: [0, 1], outputRange: [10, 0] }) }],
      }}
    >
      <Pressable
        onPress={onPress}
        accessibilityRole="button"
        accessibilityLabel={`${heading}. ${phaseLabel(phase)}. Tap to expand.`}
        style={({ pressed }) => [
          styles.bar,
          { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border },
          pressed ? styles.pressed : null,
        ]}
      >
        <Artwork uri={first.artworkUrl} size={40} radius={radius.sm} accessibilityLabel="Album art" />
        <View style={styles.body}>
          <View style={styles.topRow}>
            <Text variant="label" numberOfLines={1} style={styles.heading}>
              {heading}
            </Text>
            <ChevronUp size={18} color={theme.color.textTertiary} />
          </View>
          <Text variant="caption" tone="accent" numberOfLines={1}>
            {phaseLabel(phase)}
          </Text>
          <View style={styles.segments}>
            {ACQUISITION_PHASES.map((p, i) => {
              const filled = i <= activeIndex;
              const isActive = i === activeIndex;
              return (
                <Animated.View
                  key={p}
                  style={[
                    styles.segment,
                    {
                      backgroundColor: filled ? theme.color.accent : theme.color.border,
                      opacity: isActive ? pulse : 1,
                    },
                  ]}
                />
              );
            })}
          </View>
        </View>
      </Pressable>
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.lg,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
  body: { flex: 1, gap: spacing.xs },
  topRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  heading: { flex: 1 },
  segments: { flexDirection: 'row', gap: spacing.xs, marginTop: 2 },
  segment: { flex: 1, height: 4, borderRadius: 2 },
});
