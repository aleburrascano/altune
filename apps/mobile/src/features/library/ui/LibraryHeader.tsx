import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

export function LibraryHeader({
  initial,
  onAvatarPress,
}: {
  initial: string;
  onAvatarPress?: () => void;
}): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.header}>
      <Text variant="displayL">Library</Text>
      <Pressable
        testID="library-avatar"
        onPress={onAvatarPress}
        accessibilityRole="button"
        accessibilityLabel="Profile"
        hitSlop={8}
      >
        <View style={[styles.avatar, { backgroundColor: theme.color.accent }]}>
          <Text variant="bodyStrong" tone="onAccent">
            {initial}
          </Text>
        </View>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  avatar: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
