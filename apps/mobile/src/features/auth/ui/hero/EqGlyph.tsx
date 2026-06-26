import { View } from 'react-native';

import { useTheme } from '@shared/ui/theme';

// Static EQ bars — a small music cue above the wordmark. Always static
// (never animated), per design decision.
const BAR_HEIGHTS = [7, 14, 6, 12, 16];

export function EqGlyph() {
  const theme = useTheme();
  return (
    <View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{ flexDirection: 'row', alignItems: 'flex-end', gap: 3, height: 16 }}
    >
      {BAR_HEIGHTS.map((h, i) => (
        <View
          key={i}
          style={{ width: 3, height: h, borderRadius: 2, backgroundColor: theme.color.accent }}
        />
      ))}
    </View>
  );
}
