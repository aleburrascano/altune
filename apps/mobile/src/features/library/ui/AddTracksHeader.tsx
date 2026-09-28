import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';

import { Text, spacing } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';

type AddTracksHeaderProps = {
  playlistName: string;
  onBack: () => void;
};

export function AddTracksHeader({ playlistName, onBack }: AddTracksHeaderProps): ReactElement {
  return (
    <View style={styles.header}>
      <IconButton icon={ChevronLeft} size={24} onPress={onBack} accessibilityLabel="Back" />
      <Text variant="title" numberOfLines={1} style={styles.headerTitle}>
        Add to {playlistName}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.sm,
  },
  headerTitle: { flex: 1 },
});
