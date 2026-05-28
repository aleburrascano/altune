import type { ReactNode } from 'react';
import { View } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import { spacing } from '../theme/tokens';

export type RowProps = {
  /** Body content; fills the available width between leading and trailing. */
  children: ReactNode;
  leading?: ReactNode;
  trailing?: ReactNode;
  style?: StyleProp<ViewStyle>;
  testID?: string;
};

/** leading | body(flex) | trailing horizontal layout used by list + history rows. */
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
