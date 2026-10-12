import type { ReactElement, ReactNode } from 'react';
import { Keyboard, View, type StyleProp, type ViewStyle } from 'react-native';
import { Tappable } from '@shared/ui/primitives/Tappable';

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
    <Tappable action="ui.dismiss_keyboard" testID={testID} onPress={dismissKeyboard} style={style}>
      {children}
    </Tappable>
  );
}

export function KeepInputFocusArea(props: DismissKeyboardAreaProps): ReactElement {
  const { children, testID, style } = props;
  return (
    <View testID={testID} style={style}>
      {children}
    </View>
  );
}

export const keyboardAvoidingBehavior: 'padding' | undefined = undefined;

export const adjustsKeyboardInsets: boolean = false;
