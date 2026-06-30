/**
 * DownloadsBar — the in-flight acquisition strip of the Activity Dock.
 *
 * Renders nothing when nothing is downloading. Shows a live count + the current
 * phase ("Finding source… / Downloading… / Finishing up…") and a 3-segment
 * progress that advances by phase. Tap to expand the downloads sheet. RN
 * `Animated` only (Expo Go safe).
 */

import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, View } from 'react-native';

import { ChevronUp } from 'lucide-react-native';

import type { DownloadItem } from '@shared/acquisition/activeDownloads';
import { ACQUISITION_PHASES, phaseLabel, stageToPhase } from '@shared/acquisition/stagePhase';
import { useTrackStage } from '@shared/acquisition/stageStore';
import { Text, spacing, useTheme } from '@shared/ui';

interface DownloadsBarProps {
  items: DownloadItem[];
  onPress: () => void;
}

export function DownloadsBar({ items, onPress }: DownloadsBarProps): ReactElement | null {
  const theme = useTheme();
  const first = items[0];
  const stage = useTrackStage(first?.id ?? '');

  if (items.length === 0 || first == null) return null;

  const phase = stageToPhase(stage);
  const activeIndex = ACQUISITION_PHASES.indexOf(phase); // -1 while 'working'
  const count = items.length;
  const heading = count === 1 ? `Downloading "${first.title}"` : `Downloading ${count} songs`;

  return (
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
      <ActivityIndicator size="small" color={theme.color.accent} />
      <View style={styles.body}>
        <Text variant="caption" tone="accent" numberOfLines={1}>
          {heading}
        </Text>
        <View style={styles.segments}>
          {ACQUISITION_PHASES.map((p, i) => (
            <View
              key={p}
              style={[
                styles.segment,
                { backgroundColor: i <= activeIndex ? theme.color.accent : theme.color.border },
              ]}
            />
          ))}
        </View>
      </View>
      <Text variant="caption" tone="secondary">
        {phaseLabel(phase)}
      </Text>
      <ChevronUp size={16} color={theme.color.textTertiary} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
  body: { flex: 1, gap: 5 },
  segments: { flexDirection: 'row', gap: 3 },
  segment: { flex: 1, height: 3, borderRadius: 2 },
});
