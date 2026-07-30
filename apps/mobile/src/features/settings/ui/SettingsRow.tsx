import type { LucideIcon } from 'lucide-react-native';
import type { ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui';

export type SettingsRowTone = 'neutral' | 'accent' | 'danger' | 'success' | 'warning';

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
        styles.row,
        first ? null : { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: theme.color.border },
        disabled ? styles.disabled : null,
      ]}
    >
      <View style={[styles.glyph, { backgroundColor: theme.color.surface2 }]}>
        <Icon size={18} color={color} />
      </View>
      <View style={styles.text}>
        <Text tone={tone === 'danger' ? 'danger' : 'primary'} numberOfLines={1}>
          {label}
        </Text>
        {detail != null ? (
          <Text variant="caption" tone="tertiary" style={styles.detail} numberOfLines={2}>
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
    <Pressable
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityState={{ disabled }}
      accessibilityLabel={accessibilityLabel(label, detail)}
      style={({ pressed }) => (pressed ? { opacity: 0.6 } : null)}
    >
      {body}
    </Pressable>
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

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: minInteractiveHeight + spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  glyph: {
    width: 32,
    height: 32,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  text: { flex: 1 },
  detail: { marginTop: spacing.xs },
  disabled: { opacity: 0.5 },
});
