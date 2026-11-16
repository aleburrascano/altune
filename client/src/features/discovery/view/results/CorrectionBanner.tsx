import { StyleSheet, View } from 'react-native';

import { pressedStyle, Text, Tappable, spacing } from '@shared/ui';

interface CorrectionBannerProps {
  correctedQuery: string;
  originalQuery: string;
  onSearchOriginal: () => void;
}

export function CorrectionBanner({
  correctedQuery,
  originalQuery,
  onSearchOriginal,
}: CorrectionBannerProps) {
  return (
    <View style={styles.container}>
      <Text variant="caption" tone="secondary">
        Showing results for{' '}
        <Text variant="caption" tone="primary" style={styles.bold}>
          {correctedQuery}
        </Text>
      </Text>
      <Tappable
        action="discover.correction.search_original"
        onPress={onSearchOriginal}
        accessibilityRole="link"
        accessibilityLabel={`Search instead for ${originalQuery}`}
        hitSlop={{ top: 12, bottom: 12, left: 8, right: 8 }}
        style={({ pressed }) => [styles.link, pressedStyle(pressed)]}
      >
        <Text variant="caption" tone="accent">
          Search for “{originalQuery}”
        </Text>
      </Tappable>
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
