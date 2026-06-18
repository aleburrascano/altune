import { Pressable, StyleSheet, View } from 'react-native';
import { Search } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { spacing, radius } from '@shared/ui/theme/tokens';
import type { DiscoverySuggestion } from '@shared/api-client/discovery';

interface SuggestionsListProps {
  suggestions: DiscoverySuggestion[];
  onSelect: (text: string) => void;
}

export function SuggestionsList({ suggestions, onSelect }: SuggestionsListProps) {
  const { color } = useTheme();

  if (suggestions.length === 0) {
    return null;
  }

  return (
    <View style={[styles.container, { backgroundColor: color.surface1, borderTopColor: color.border }]}>
      {suggestions.map((s, i) => (
        <Pressable
          key={`${s.text}-${i}`}
          style={({ pressed }) => [
            styles.row,
            i > 0 ? { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: color.border } : null,
            pressed ? { opacity: 0.7 } : null,
          ]}
          onPress={() => onSelect(s.text)}
          accessibilityRole="button"
          accessibilityLabel={`${s.text}, ${s.kind}`}
        >
          <View style={[styles.iconWrap, { backgroundColor: color.surface2 }]}>
            <Search size={14} color={color.textTertiary} />
          </View>
          <Text variant="body" numberOfLines={1} style={styles.text}>
            {s.text}
          </Text>
          <Text variant="caption" tone="tertiary">
            {s.kind}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    position: 'absolute',
    top: '100%',
    left: 0,
    right: 0,
    borderBottomLeftRadius: radius.md,
    borderBottomRightRadius: radius.md,
    borderTopWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
    maxHeight: 5 * 44,
    zIndex: 20,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 44,
  },
  iconWrap: {
    width: 28,
    height: 28,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  text: {
    flex: 1,
  },
});
