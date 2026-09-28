import { StyleSheet, type ViewStyle } from 'react-native';

const styles = StyleSheet.create({
  pressed: { opacity: 0.7 },
});

export function pressedStyle(pressed: boolean): ViewStyle | null {
  return pressed ? styles.pressed : null;
}
