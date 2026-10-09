import type { ReactElement, ReactNode } from 'react';
import { Keyboard, Platform, Pressable, type StyleProp, type ViewStyle } from 'react-native';

export function dismissKeyboard(): void {
  Keyboard.dismiss();
}

export type DismissKeyboardAreaProps = {
  children: ReactNode;
  testID?: string;
  style?: StyleProp<ViewStyle>;
};

export function DismissKeyboardArea(props: DismissKeyboardAreaProps): ReactElement {
  const { children, testID, style } = props;
  return (
    <Pressable testID={testID} onPress={dismissKeyboard} style={style}>
      {children}
    </Pressable>
  );
}

export const keyboardAvoidingBehavior: 'padding' | undefined =
  Platform.OS === 'ios' ? 'padding' : undefined;

export const adjustsKeyboardInsets: boolean = Platform.OS === 'ios';
