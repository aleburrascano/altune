import { ShieldCheck } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { useAnalyticsPreference } from '@shared/device/analyticsPreference';
import { Text, radius, spacing, useTheme } from '@shared/ui';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { SettingsCard, SettingsRow } from '@features/settings/ui/SettingsCard';

export function AnalyticsCard(): ReactElement {
  const shareAnalytics = useAnalyticsPreference((s) => s.shareAnalytics);
  const setShareAnalytics = useAnalyticsPreference((s) => s.setShareAnalytics);
  return (
    <SettingsCard label="Privacy">
      <SettingsRow {...analyticsRowProps(shareAnalytics, setShareAnalytics)} />
    </SettingsCard>
  );
}

function analyticsRowProps(on: boolean, setOn: (on: boolean) => void) {
  return {
    first: true,
    icon: ShieldCheck,
    label: 'Share usage analytics',
    detail: 'Helps improve altune. Your activity still personalizes your results.',
    right: <AnalyticsSegment on={on} onSelect={setOn} />,
  };
}

type AnalyticsSegmentProps = {
  on: boolean;
  onSelect: (on: boolean) => void;
};

const OPTIONS: { on: boolean; label: string }[] = [
  { on: true, label: 'On' },
  { on: false, label: 'Off' },
];

type AnalyticsOptionProps = AnalyticsSegmentProps & { option: (typeof OPTIONS)[number] };

function isSelected({ option, on }: AnalyticsOptionProps): boolean {
  return option.on === on;
}

function optionStyle(selected: boolean, canvas: string) {
  return [segmentStyles.option, selected ? { backgroundColor: canvas } : null];
}

function optionIdentityProps({ option, onSelect }: AnalyticsOptionProps) {
  return {
    action: 'settings.analytics.select',
    testID: `settings-analytics-${option.on ? 'on' : 'off'}`,
    onPress: () => onSelect(option.on),
    accessibilityLabel: `Share usage analytics ${option.label}`,
  };
}

function optionPressableProps(props: AnalyticsOptionProps, canvas: string) {
  const selected = isSelected(props);
  return {
    ...optionIdentityProps(props),
    accessibilityRole: 'radio' as const,
    accessibilityState: { selected },
    style: optionStyle(selected, canvas),
  };
}

function AnalyticsOption(props: AnalyticsOptionProps): ReactElement {
  const { color } = useTheme();
  return (
    <Tappable {...optionPressableProps(props, color.canvas)}>
      <Text variant="caption" tone={isSelected(props) ? 'primary' : 'tertiary'}>
        {props.option.label}
      </Text>
    </Tappable>
  );
}

function AnalyticsSegment({ on, onSelect }: AnalyticsSegmentProps): ReactElement {
  const { color } = useTheme();
  return (
    <View style={[segmentStyles.track, { backgroundColor: color.surface2 }]}>
      {OPTIONS.map((option) => (
        <AnalyticsOption key={option.label} option={option} on={on} onSelect={onSelect} />
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
