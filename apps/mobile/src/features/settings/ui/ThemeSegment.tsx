import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, radius, spacing, useTheme } from '@shared/ui';
import type { ColorScheme } from '@shared/ui';

type ThemeSegmentProps = {
  scheme: ColorScheme;
  onSelect: (scheme: ColorScheme) => void;
};

const OPTIONS: { scheme: ColorScheme; label: string }[] = [
  { scheme: 'dark', label: 'Dark' },
  { scheme: 'light', label: 'Light' },
];

export function ThemeSegment({ scheme, onSelect }: ThemeSegmentProps): ReactElement {
  const theme = useTheme();
  return (
    <View style={[styles.track, { backgroundColor: theme.color.surface2 }]}>
      {OPTIONS.map((option) => {
        const selected = option.scheme === scheme;
        return (
          <Pressable
            key={option.scheme}
            testID={`settings-theme-${option.scheme}`}
            onPress={() => onSelect(option.scheme)}
            accessibilityRole="radio"
            accessibilityState={{ selected }}
            accessibilityLabel={`${option.label} theme`}
            style={[
              styles.option,
              selected ? { backgroundColor: theme.color.canvas } : null,
            ]}
          >
            <Text variant="caption" tone={selected ? 'primary' : 'tertiary'}>
              {option.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  track: { flexDirection: 'row', borderRadius: radius.sm, padding: 2 },
  option: {
    minHeight: 32,
    paddingHorizontal: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
