/**
 * Disclosure — a single collapsible row for deep, secondary metadata.
 *
 * The default detail screen stays clean; provider deep-cuts (album credits /
 * label, artist bio / links) live behind one tap here instead of as always-on
 * slabs. Collapsed by default.
 */

import { useState, type ReactElement, type ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronDown, ChevronRight } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';
import { spacing } from '@shared/ui/theme/tokens';

export function Disclosure({
  label,
  testID,
  children,
}: {
  label: string;
  testID?: string;
  children: ReactNode;
}): ReactElement {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  return (
    <View style={[styles.wrap, { borderColor: theme.color.border }]}>
      <Pressable
        testID={testID}
        onPress={() => setOpen((prev) => !prev)}
        accessibilityRole="button"
        accessibilityLabel={open ? `Collapse ${label}` : `Expand ${label}`}
        style={({ pressed }) => [styles.header, pressed ? { opacity: 0.6 } : null]}
      >
        <Text variant="label" tone="secondary">
          {label}
        </Text>
        {open ? (
          <ChevronDown size={18} color={theme.color.textSecondary} />
        ) : (
          <ChevronRight size={18} color={theme.color.textSecondary} />
        )}
      </Pressable>
      {open ? <View style={styles.body}>{children}</View> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    marginTop: spacing.xl,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: spacing.lg,
    minHeight: 48,
  },
  body: { paddingBottom: spacing.md },
});
