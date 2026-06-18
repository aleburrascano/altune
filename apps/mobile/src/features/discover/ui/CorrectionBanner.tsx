import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

interface CorrectionBannerProps {
  correctedQuery: string;
  originalQuery: string;
  onSearchOriginal: () => void;
}

export function CorrectionBanner({ correctedQuery, originalQuery, onSearchOriginal }: CorrectionBannerProps) {
  return (
    <View style={styles.container}>
      <Text variant="caption" tone="secondary">
        Showing results for{' '}
        <Text variant="caption" tone="primary" style={styles.bold}>
          {correctedQuery}
        </Text>
      </Text>
      <Pressable
        onPress={onSearchOriginal}
        accessibilityRole="link"
        accessibilityLabel={`Search instead for ${originalQuery}`}
        hitSlop={{ top: 12, bottom: 12, left: 8, right: 8 }}
        style={({ pressed }) => [styles.link, pressed ? { opacity: 0.7 } : null]}
      >
        <Text variant="caption" tone="accent">
          Search for &ldquo;{originalQuery}&rdquo;
        </Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: spacing.sm,
    paddingVertical: spacing.sm,
  },
  bold: {
    fontWeight: '600',
  },
  link: {
    paddingVertical: spacing.xs,
  },
});
