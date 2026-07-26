import type { ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme/tokens';

export function Section({
  label,
  action,
  testID,
  children,
}: {
  label: string;
  action?: { label: string; onPress: () => void; testID?: string };
  testID?: string;
  children: ReactNode;
}): ReactElement {
  return (
    <View testID={testID} style={styles.section}>
      <View style={styles.head}>
        <Text variant="overline" tone="tertiary">
          {label.toUpperCase()}
        </Text>
        {action != null ? (
          <Pressable
            testID={action.testID}
            onPress={action.onPress}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={action.label}
            style={({ pressed }) => (pressed ? styles.pressed : null)}
          >
            <Text variant="caption" tone="accent">
              {action.label}
            </Text>
          </Pressable>
        ) : null}
      </View>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing['2xl'] },
  head: {
    flexDirection: 'row',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
    minHeight: 20,
  },
  pressed: { opacity: 0.6 },
});
