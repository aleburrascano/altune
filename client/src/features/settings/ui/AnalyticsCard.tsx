import { ShieldCheck } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { useAnalyticsPreference } from '@shared/device/analyticsPreference';
import {
  SettingsCard,
  SettingsRow,
  SettingsSegment,
  type SettingsSegmentOption,
} from '@features/settings/ui/SettingsCard';

export function AnalyticsCard(): ReactElement {
  const shareAnalytics = useAnalyticsPreference((s) => s.shareAnalytics);
  const setShareAnalytics = useAnalyticsPreference((s) => s.setShareAnalytics);
  return (
    <SettingsCard label="Privacy">
      <SettingsRow {...analyticsRowProps(shareAnalytics, setShareAnalytics)} />
    </SettingsCard>
  );
}

const OPTIONS: SettingsSegmentOption<boolean>[] = [
  {
    value: true,
    label: 'On',
    testID: 'settings-analytics-on',
    accessibilityLabel: 'Share usage analytics On',
  },
  {
    value: false,
    label: 'Off',
    testID: 'settings-analytics-off',
    accessibilityLabel: 'Share usage analytics Off',
  },
];

const SEGMENT = { action: 'settings.analytics.select', options: OPTIONS };

function analyticsRowProps(on: boolean, setOn: (on: boolean) => void) {
  return {
    first: true,
    icon: ShieldCheck,
    label: 'Share usage analytics',
    detail: 'Helps improve altune. Your activity still personalizes your results.',
    right: <SettingsSegment {...SEGMENT} value={on} onSelect={setOn} />,
  };
}
