import { StyleSheet, View } from 'react-native';
import { Search } from 'lucide-react-native';

import { KeepInputFocusArea } from '@shared/ui/keyboard/keyboard';
import { pressedStyle } from '@shared/ui/primitives/pressedStyle';
import { Text } from '@shared/ui/primitives/Text';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { useTheme } from '@shared/ui/theme/useTheme';
import { spacing, radius, touchTarget, zLayer } from '@shared/ui/theme/tokens';
import type { DiscoverySuggestion } from '@shared/apiClient/discovery';

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
    <KeepInputFocusArea
      style={[styles.container, { backgroundColor: color.surface1, borderTopColor: color.border }]}
    >
      {suggestions.map((s, i) => (
        <Tappable
          action="discover.suggestion.select"
          key={`${s.text}-${i}`}
          style={({ pressed }) => [
            styles.row,
            i > 0
              ? { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: color.border }
              : null,
            pressedStyle(pressed),
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
        </Tappable>
      ))}
    </KeepInputFocusArea>
  );
}

const VISIBLE_SUGGESTION_ROWS = 5;
const SUGGESTION_ICON_SIZE = 28;

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
    maxHeight: VISIBLE_SUGGESTION_ROWS * touchTarget,
    zIndex: zLayer.suggestions,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: touchTarget,
  },
  iconWrap: {
    width: SUGGESTION_ICON_SIZE,
    height: SUGGESTION_ICON_SIZE,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  text: {
    flex: 1,
  },
});
