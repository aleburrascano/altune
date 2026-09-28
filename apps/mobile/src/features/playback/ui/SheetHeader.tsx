import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { ChevronDown } from 'lucide-react-native';

import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

export function SheetScreen({
  testID,
  children,
}: {
  testID?: string;
  children: ReactNode;
}): ReactElement {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <View
      testID={testID}
      style={[styles.container, { backgroundColor: theme.color.canvas, paddingTop: insets.top }]}
    >
      {children}
    </View>
  );
}

export function SheetHeader({
  onClose,
  closeLabel,
  children,
}: {
  onClose: () => void;
  closeLabel: string;
  children: ReactNode;
}): ReactElement {
  return (
    <View style={styles.header}>
      <View style={styles.leading}>
        <IconButton
          icon={ChevronDown}
          size={28}
          onPress={onClose}
          accessibilityLabel={closeLabel}
        />
      </View>
      {children}
    </View>
  );
}

export function SheetHeaderCenter({ children }: { children: ReactNode }): ReactElement {
  return <View style={styles.center}>{children}</View>;
}

export function SheetHeaderTrailing({ children }: { children?: ReactNode }): ReactElement {
  return <View style={styles.trailing}>{children}</View>;
}

const SIDE_MIN_WIDTH = 44;

const styles = StyleSheet.create({
  container: { flex: 1 },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.lg,
    gap: spacing.md,
  },
  leading: { flex: 1, minWidth: SIDE_MIN_WIDTH, alignItems: 'flex-start' },
  center: { flexShrink: 1, alignItems: 'center' },
  trailing: {
    flex: 1,
    minWidth: SIDE_MIN_WIDTH,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    gap: spacing.xs,
  },
});
