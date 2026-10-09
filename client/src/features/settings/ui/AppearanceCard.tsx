import { Moon } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { useThemePreference } from '@shared/ui/theme/themePreference';
import { Text, radius, spacing, useTheme } from '@shared/ui';
import type { ColorScheme } from '@shared/ui';
import { SettingsCard, SettingsRow } from '@features/settings/ui/SettingsCard';

export function AppearanceCard(): ReactElement {
  const scheme = useThemePreference((s) => s.scheme);
  const setScheme = useThemePreference((s) => s.setScheme);
  return (
    <SettingsCard label="Appearance">
      <SettingsRow {...appearanceRowProps(scheme, setScheme)} />
    </SettingsCard>
  );
}

function appearanceRowProps(scheme: ColorScheme, setScheme: (scheme: ColorScheme) => void) {
  return {
    first: true,
    icon: Moon,
    label: 'Theme',
    detail: scheme === 'light' ? 'Light mode has no design pass yet (ADR-0008)' : undefined,
    right: <ThemeSegment scheme={scheme} onSelect={setScheme} />,
  };
}

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
    <View style={[segmentStyles.track, { backgroundColor: theme.color.surface2 }]}>
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
              segmentStyles.option,
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

const segmentStyles = StyleSheet.create({
  track: { flexDirection: 'row', borderRadius: radius.sm, padding: 2 },
  option: {
    minHeight: 32,
    paddingHorizontal: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
