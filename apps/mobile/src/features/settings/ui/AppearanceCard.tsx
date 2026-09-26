import { Moon } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { useThemePreference } from '@shared/ui/theme/themePreference';
import type { ColorScheme } from '@shared/ui';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';
import { ThemeSegment } from './ThemeSegment';

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
