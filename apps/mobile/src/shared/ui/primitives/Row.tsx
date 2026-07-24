import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import { spacing } from '../theme/tokens';

export type RowProps = {
  children: ReactNode;
  leading?: ReactNode;
  trailing?: ReactNode;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

export function Row({ children, leading, trailing, style, testID }: RowProps) {
  return (
    <View
      testID={testID}
      style={[{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }, style]}
    >
      {leading != null ? leading : null}
      <View style={{ flex: 1 }}>{children}</View>
      {trailing != null ? trailing : null}
    </View>
  );
}
