import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';

export type IconBadgeProps = {
  size: number;
  radius: number;
  background: string;
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
};

export function IconBadge({
  size,
  radius,
  background,
  children,
  style,
}: IconBadgeProps): ReactElement {
  return (
    <View
      style={StyleSheet.flatten([
        {
          width: size,
          height: size,
          borderRadius: radius,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: background,
        },
        style,
      ])}
    >
      {children}
    </View>
  );
}
