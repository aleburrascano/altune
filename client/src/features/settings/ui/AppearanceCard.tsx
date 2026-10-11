import { Moon } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { useThemePreference } from '@shared/ui/theme/themePreference';
import { Text, radius, spacing, useTheme } from '@shared/ui';
import type { ColorScheme } from '@shared/ui';
import { Tappable } from '@shared/ui/primitives/Tappable';
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

type ThemeOptionProps = ThemeSegmentProps & { option: (typeof OPTIONS)[number] };

function isSelected({ option, scheme }: ThemeOptionProps): boolean {
  return option.scheme === scheme;
}

function optionStyle(selected: boolean, canvas: string) {
  return [segmentStyles.option, selected ? { backgroundColor: canvas } : null];
}

function optionIdentityProps({ option, onSelect }: ThemeOptionProps) {
  return {
    action: 'settings.theme.select',
    testID: `settings-theme-${option.scheme}`,
    onPress: () => onSelect(option.scheme),
    accessibilityLabel: `${option.label} theme`,
  };
}

function optionPressableProps(props: ThemeOptionProps, canvas: string) {
  const selected = isSelected(props);
  return {
    ...optionIdentityProps(props),
    accessibilityRole: 'radio' as const,
    accessibilityState: { selected },
    style: optionStyle(selected, canvas),
  };
}

function ThemeOption(props: ThemeOptionProps): ReactElement {
  const { color } = useTheme();
  return (
    <Tappable {...optionPressableProps(props, color.canvas)}>
      <Text variant="caption" tone={isSelected(props) ? 'primary' : 'tertiary'}>
        {props.option.label}
      </Text>
    </Tappable>
  );
}

function ThemeSegment({ scheme, onSelect }: ThemeSegmentProps): ReactElement {
  const { color } = useTheme();
  return (
    <View style={[segmentStyles.track, { backgroundColor: color.surface2 }]}>
      {OPTIONS.map((option) => (
        <ThemeOption key={option.scheme} option={option} scheme={scheme} onSelect={onSelect} />
      ))}
    </View>
  );
}

const SEGMENT_PADDING = 2;
const SEGMENT_MIN_HEIGHT = 32;

const segmentStyles = StyleSheet.create({
  track: { flexDirection: 'row', borderRadius: radius.sm, padding: SEGMENT_PADDING },
  option: {
    minHeight: SEGMENT_MIN_HEIGHT,
    paddingHorizontal: spacing.md,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
