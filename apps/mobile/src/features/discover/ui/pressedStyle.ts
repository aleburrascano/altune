import { StyleSheet, type ViewStyle } from 'react-native';

const styles = StyleSheet.create({
  pressed: { opacity: 0.7 },
});

/** The one press feedback for discover's Pressables; a site adding more composes it in a style array. */
export function pressedStyle(pressed: boolean): ViewStyle | null {
  return pressed ? styles.pressed : null;
}
