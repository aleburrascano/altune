import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { tokens } from '@shared/ui/theme/tokens';

interface CorrectionBannerProps {
  correctedQuery: string;
  originalQuery: string;
  onSearchOriginal: () => void;
}

export function CorrectionBanner({ correctedQuery, originalQuery, onSearchOriginal }: CorrectionBannerProps) {
  const { color } = useTheme();

  return (
    <View style={[styles.container, { backgroundColor: color.surface1 }]}>
      <Text style={[styles.label, { color: color.textSecondary }]}>
        Showing results for{' '}
        <Text style={{ color: color.textPrimary, fontWeight: '600' }}>
          {correctedQuery}
        </Text>
      </Text>
      <Pressable
        onPress={onSearchOriginal}
        accessibilityRole="button"
        accessibilityLabel={`Search instead for ${originalQuery}`}
        hitSlop={8}
      >
        <Text style={[styles.link, { color: color.accent }]}>
          Search instead for &quot;{originalQuery}&quot;
        </Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    paddingHorizontal: tokens.spacing.md,
    paddingVertical: tokens.spacing.sm,
    gap: 2,
  },
  label: {
    fontSize: 13,
  },
  link: {
    fontSize: 13,
  },
});
