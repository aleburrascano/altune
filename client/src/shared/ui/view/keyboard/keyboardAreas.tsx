import type { ReactElement, ReactNode } from 'react';
import { Keyboard, View, type StyleProp, type ViewStyle } from 'react-native';
import { Tappable } from '@shared/ui/view/primitives/Tappable';

export function dismissKeyboard(): void {
  Keyboard.dismiss();
}

export type DismissKeyboardAreaProps = {
  children: ReactNode;
  testID?: string;
  style?: StyleProp<ViewStyle>;
  onDismiss?: () => void;
};

export function DismissKeyboardArea(props: DismissKeyboardAreaProps): ReactElement {
  const { children, testID, style, onDismiss = dismissKeyboard } = props;
  return (
    <Tappable action="ui.dismiss_keyboard" testID={testID} onPress={onDismiss} style={style}>
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
