import type { ReactElement } from 'react';
import { StyleSheet, type StyleProp, type TextStyle } from 'react-native';

import { Text } from '@shared/ui';

export function SectionLabel({
  children,
  style,
}: {
  children: string;
  style?: StyleProp<TextStyle>;
}): ReactElement {
  return (
    <Text variant="label" tone="tertiary" style={[styles.label, style]}>
      {children}
    </Text>
  );
}

const styles = StyleSheet.create({
  label: { letterSpacing: 1 },
});
