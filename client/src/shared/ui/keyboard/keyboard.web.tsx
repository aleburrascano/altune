import type { ReactElement, ReactNode } from 'react';
import { View, type StyleProp, type ViewStyle } from 'react-native';

export function dismissKeyboard(): void {}

export type DismissKeyboardAreaProps = {
  children: ReactNode;
  testID?: string;
  style?: StyleProp<ViewStyle>;
};

export function DismissKeyboardArea(props: DismissKeyboardAreaProps): ReactElement {
  const { children, testID, style } = props;
  return (
    <View testID={testID} style={style}>
      {children}
    </View>
  );
}

export const keyboardAvoidingBehavior: 'padding' | undefined = undefined;

export const adjustsKeyboardInsets: boolean = false;
