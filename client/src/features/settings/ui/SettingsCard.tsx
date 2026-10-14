import type { LucideIcon } from 'lucide-react-native';
import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { IconBadge, Text, minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { opacity } from '@shared/ui/theme';

type SettingsCardProps = {
  label?: string;
  danger?: boolean;
  children: ReactNode;
};

function SettingsCardLabel({ label, danger }: { label: string; danger: boolean }): ReactElement {
  return (
    <Text variant="overline" tone={danger ? 'danger' : 'tertiary'} style={cardStyles.label}>
      {label}
    </Text>
  );
}

function cardSurfaceStyle(theme: ReturnType<typeof useTheme>, danger: boolean) {
  return [
    cardStyles.card,
    {
      backgroundColor: theme.color.surface1,
      borderColor: danger ? theme.color.danger : theme.color.border,
    },
  ];
}

export function SettingsCard({ label, danger = false, children }: SettingsCardProps): ReactElement {
  const theme = useTheme();
  return (
    <View style={cardStyles.group}>
      {label != null ? <SettingsCardLabel label={label} danger={danger} /> : null}
      <View style={cardSurfaceStyle(theme, danger)}>{children}</View>
    </View>
  );
}

const cardStyles = StyleSheet.create({
  group: { marginTop: spacing.xl },
  label: { marginBottom: spacing.sm, marginLeft: spacing.xs },
  card: { borderRadius: radius.lg, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
});

type SettingsRowTone = 'neutral' | 'accent' | 'danger' | 'success' | 'warning';

type SettingsRowProps = {
  icon: LucideIcon;
  label: string;
  detail?: string | undefined;
  tone?: SettingsRowTone;
  right?: ReactNode;
  onPress?: (() => void) | undefined;
  disabled?: boolean;
  first?: boolean;
  testID?: string;
};

export function SettingsRow({
  icon: Icon,
  label,
  detail,
  tone = 'neutral',
  right,
  onPress,
  disabled = false,
  first = false,
  testID,
}: SettingsRowProps): ReactElement {
  const theme = useTheme();
  const color = toneColor(theme.color, tone);

  const body = (
    <View
      style={[
        rowStyles.row,
        first
          ? null
          : { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: theme.color.border },
        disabled ? rowStyles.disabled : null,
      ]}
    >
      <IconBadge size={32} radius={radius.sm} background={theme.color.surface2}>
        <Icon size={18} color={color} />
      </IconBadge>
      <View style={rowStyles.text}>
        <Text tone={tone === 'danger' ? 'danger' : 'primary'} numberOfLines={1}>
          {label}
        </Text>
        {detail != null ? (
          <Text variant="caption" tone="tertiary" style={rowStyles.detail} numberOfLines={2}>
            {detail}
          </Text>
        ) : null}
      </View>
      {right}
    </View>
  );

  if (onPress == null) {
    return (
      <View testID={testID} accessible accessibilityLabel={accessibilityLabel(label, detail)}>
        {body}
      </View>
    );
  }

  return (
    <Tappable
      action="settings.row.press"
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      accessibilityLabel={accessibilityLabel(label, detail)}
      style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
    >
      {body}
    </Tappable>
  );
}

function accessibilityLabel(label: string, detail?: string): string {
  return detail == null ? label : `${label}, ${detail}`;
}

type RowColors = {
  textSecondary: string;
  accentText: string;
  danger: string;
  success: string;
  warning: string;
};

function toneColor(color: RowColors, tone: SettingsRowTone): string {
  switch (tone) {
    case 'accent':
      return color.accentText;
    case 'danger':
      return color.danger;
    case 'success':
      return color.success;
    case 'warning':
      return color.warning;
    case 'neutral':
      return color.textSecondary;
  }
}

const rowStyles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: minInteractiveHeight + spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  text: { flex: 1 },
  detail: { marginTop: spacing.xs },
  disabled: { opacity: opacity.disabled },
});

export type SettingsSegmentOption<T> = {
  value: T;
  label: string;
  testID: string;
  accessibilityLabel: string;
};

type SettingsSegmentProps<T> = {
  action: string;
  options: readonly SettingsSegmentOption<T>[];
  value: T;
  onSelect: (value: T) => void;
};

type SegmentOptionProps<T> = Omit<SettingsSegmentProps<T>, 'options'> & {
  option: SettingsSegmentOption<T>;
};

function isSegmentSelected<T>({ option, value }: SegmentOptionProps<T>): boolean {
  return option.value === value;
}

function segmentOptionStyle(selected: boolean, canvas: string) {
  return [segmentStyles.option, selected ? { backgroundColor: canvas } : null];
}

function segmentOptionIdentityProps<T>({ action, option, onSelect }: SegmentOptionProps<T>) {
  return {
    action,
    testID: option.testID,
    onPress: () => onSelect(option.value),
    accessibilityLabel: option.accessibilityLabel,
  };
}

function segmentOptionProps<T>(props: SegmentOptionProps<T>, canvas: string) {
  const selected = isSegmentSelected(props);
  return {
    ...segmentOptionIdentityProps(props),
    accessibilityRole: 'radio' as const,
    accessibilityState: { selected },
    style: segmentOptionStyle(selected, canvas),
  };
}

function SegmentOption<T>(props: SegmentOptionProps<T>): ReactElement {
  const { color } = useTheme();
  return (
    <Tappable {...segmentOptionProps(props, color.canvas)}>
      <Text variant="caption" tone={isSegmentSelected(props) ? 'primary' : 'tertiary'}>
        {props.option.label}
      </Text>
    </Tappable>
  );
}

export function SettingsSegment<T>({ options, ...rest }: SettingsSegmentProps<T>): ReactElement {
  const { color } = useTheme();
  return (
    <View style={[segmentStyles.track, { backgroundColor: color.surface2 }]}>
      {options.map((option) => (
        <SegmentOption key={option.testID} option={option} {...rest} />
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
