import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, radius, spacing, useTheme } from '@shared/ui';

type SettingsCardProps = {
  label?: string;
  danger?: boolean;
  children: ReactNode;
};

function SettingsCardLabel({ label, danger }: { label: string; danger: boolean }): ReactElement {
  return (
    <Text variant="overline" tone={danger ? 'danger' : 'tertiary'} style={styles.label}>
      {label}
    </Text>
  );
}

function cardSurfaceStyle(theme: ReturnType<typeof useTheme>, danger: boolean) {
  return [
    styles.card,
    {
      backgroundColor: theme.color.surface1,
      borderColor: danger ? theme.color.danger : theme.color.border,
    },
  ];
}

export function SettingsCard({ label, danger = false, children }: SettingsCardProps): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.group}>
      {label != null ? <SettingsCardLabel label={label} danger={danger} /> : null}
      <View style={cardSurfaceStyle(theme, danger)}>{children}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  group: { marginTop: spacing.xl },
  label: { marginBottom: spacing.sm, marginLeft: spacing.xs },
  card: { borderRadius: radius.lg, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
});
