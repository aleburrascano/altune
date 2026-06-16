import { Pressable, StyleSheet, View } from 'react-native';

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
    <View style={[styles.container, { backgroundColor: color.surface1, borderColor: color.border }]}>
      {suggestions.map((s, i) => (
        <Pressable
          key={`${s.text}-${i}`}
          style={[styles.row, i > 0 && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: color.border }]}
          onPress={() => onSelect(s.text)}
          accessibilityRole="button"
          accessibilityLabel={`Suggestion: ${s.text}`}
        >
          <Text style={[styles.kindLabel, { color: color.textTertiary }]}>
            {s.kind}
          </Text>
          <Text style={[styles.text, { color: color.textPrimary }]} numberOfLines={1}>
            {s.text}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    borderRadius: radius.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: 'hidden',
    marginHorizontal: spacing.md,
    marginTop: spacing.xs,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm + 2,
    gap: spacing.sm,
    minHeight: 44,
  },
  kindLabel: {
    fontSize: 11,
    textTransform: 'uppercase',
    width: 48,
  },
  text: {
    flex: 1,
    fontSize: 15,
  },
});
