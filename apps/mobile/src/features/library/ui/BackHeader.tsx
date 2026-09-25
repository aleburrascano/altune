import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';

import { spacing } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';

type BackHeaderProps = {
  onBack: () => void;
  children?: ReactNode;
};

export function BackHeader({ onBack, children }: BackHeaderProps): ReactElement {
  return (
    <View style={styles.header}>
      <IconButton icon={ChevronLeft} size={24} onPress={onBack} accessibilityLabel="Back" />
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingTop: spacing.xs,
    paddingHorizontal: spacing.lg,
  },
});
