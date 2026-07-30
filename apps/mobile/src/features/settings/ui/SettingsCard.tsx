import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, radius, spacing, useTheme } from '@shared/ui';

type SettingsCardProps = {
  label?: string;
  danger?: boolean;
  children: ReactNode;
};

export function SettingsCard({
  label,
  danger = false,
  children,
}: SettingsCardProps): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.group}>
      {label != null ? (
        <Text
          variant="overline"
          tone={danger ? 'danger' : 'tertiary'}
          style={styles.label}
        >
          {label}
        </Text>
      ) : null}
      <View
        style={[
          styles.card,
          {
            backgroundColor: theme.color.surface1,
            borderColor: danger ? theme.color.danger : theme.color.border,
          },
        ]}
      >
        {children}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  group: { marginTop: spacing.xl },
  label: { marginBottom: spacing.sm, marginLeft: spacing.xs },
  card: { borderRadius: radius.lg, borderWidth: StyleSheet.hairlineWidth, overflow: 'hidden' },
});
