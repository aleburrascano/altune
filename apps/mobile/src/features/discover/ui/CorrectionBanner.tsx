import { Pressable, StyleSheet, View } from 'react-native';
import { Info } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { spacing, radius } from '@shared/ui/theme/tokens';

interface CorrectionBannerProps {
  correctedQuery: string;
  originalQuery: string;
  onSearchOriginal: () => void;
}

export function CorrectionBanner({ correctedQuery, originalQuery, onSearchOriginal }: CorrectionBannerProps) {
  const { color } = useTheme();

  return (
    <View style={[styles.container, { backgroundColor: color.surface1 }]}>
      <View style={styles.row}>
        <Info size={14} color={color.textTertiary} />
        <Text variant="caption" tone="secondary">
          Showing results for{' '}
          <Text variant="caption" tone="primary" style={styles.bold}>
            {correctedQuery}
          </Text>
        </Text>
      </View>
      <Pressable
        onPress={onSearchOriginal}
        accessibilityRole="link"
        accessibilityLabel={`Search instead for ${originalQuery}`}
        hitSlop={8}
        style={({ pressed }) => (pressed ? { opacity: 0.7 } : null)}
      >
        <Text variant="caption" tone="accent" style={styles.link}>
          Search instead for &ldquo;{originalQuery}&rdquo;
        </Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    borderRadius: radius.lg,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    gap: spacing.xs,
    marginTop: spacing.sm,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  bold: {
    fontWeight: '600',
  },
  link: {
    textDecorationLine: 'underline',
    marginLeft: 22, // aligned with text after the info icon
  },
});
