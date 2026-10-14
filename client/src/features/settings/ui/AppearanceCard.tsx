import { Moon } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { useThemePreference } from '@shared/ui/theme/themePreference';
import type { ColorScheme } from '@shared/ui';
import {
  SettingsCard,
  SettingsRow,
  SettingsSegment,
  type SettingsSegmentOption,
} from '@features/settings/ui/SettingsCard';

export function AppearanceCard(): ReactElement {
  const scheme = useThemePreference((s) => s.scheme);
  const setScheme = useThemePreference((s) => s.setScheme);
  return (
    <SettingsCard label="Appearance">
      <SettingsRow {...appearanceRowProps(scheme, setScheme)} />
    </SettingsCard>
  );
}

const OPTIONS: SettingsSegmentOption<ColorScheme>[] = [
  {
    value: 'dark',
    label: 'Dark',
    testID: 'settings-theme-dark',
    accessibilityLabel: 'Dark theme',
  },
  {
    value: 'light',
    label: 'Light',
    testID: 'settings-theme-light',
    accessibilityLabel: 'Light theme',
  },
];

const SEGMENT = { action: 'settings.theme.select', options: OPTIONS };

function appearanceRowProps(scheme: ColorScheme, setScheme: (scheme: ColorScheme) => void) {
  return {
    first: true,
    icon: Moon,
    label: 'Theme',
    detail: scheme === 'light' ? 'Light mode has no design pass yet (ADR-0008)' : undefined,
    right: <SettingsSegment {...SEGMENT} value={scheme} onSelect={setScheme} />,
  };
}
