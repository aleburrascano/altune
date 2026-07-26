import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import type { TextTone } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

export type DetailFact = {
  label: string;
  value: string;
  tone?: TextTone;
};

export function DetailFacts({
  facts,
  testID,
}: {
  facts: readonly (DetailFact | null)[];
  testID?: string;
}): ReactElement | null {
  const theme = useTheme();
  const present = facts.filter((fact): fact is DetailFact => fact !== null);

  if (present.length === 0) {
    return null;
  }

  return (
    <View testID={testID} style={[styles.row, { borderTopColor: theme.color.border }]}>
      {present.map((fact) => (
        <View key={fact.label} style={styles.cell}>
          <Text variant="overline" tone="tertiary">
            {fact.label.toUpperCase()}
          </Text>
          <Text variant="bodyStrong" tone={fact.tone ?? 'primary'} style={styles.value}>
            {fact.value}
          </Text>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.md,
    marginTop: spacing.xl,
  },
  cell: { flexShrink: 1 },
  value: { marginTop: 1 },
});
