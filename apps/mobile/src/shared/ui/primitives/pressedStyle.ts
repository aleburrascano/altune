import { StyleSheet, type ViewStyle } from 'react-native';

export const PRESSED_OPACITY = 0.7;

const styles = StyleSheet.create({
  pressed: { opacity: PRESSED_OPACITY },
});

export function pressedStyle(pressed: boolean): ViewStyle | null {
  return pressed ? styles.pressed : null;
}
