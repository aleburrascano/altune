import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

export function PlaylistPlaceholder(): ReactElement {
  const theme = useTheme();
  return (
    <View
      testID="library-playlist-placeholder"
      style={[styles.card, { borderColor: theme.color.border }]}
    >
      <Text variant="label" tone="secondary">
        Organize your music into playlists
      </Text>
      <View style={[styles.badge, { backgroundColor: theme.color.accent }]}>
        <Text variant="caption" tone="onAccent">
          Coming Soon
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    marginHorizontal: spacing.lg,
    padding: spacing.lg,
    borderRadius: 12,
    borderWidth: 1,
    borderStyle: 'dashed',
    alignItems: 'center',
    gap: spacing.sm,
  },
  badge: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
    borderRadius: 12,
  },
});
