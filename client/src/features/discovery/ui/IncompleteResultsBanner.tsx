import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { AlertTriangle } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';

const INCOMPLETE_RESULTS_MESSAGE =
  'Some results may be missing. A music source is unavailable right now.';

export function IncompleteResultsBanner({
  visible,
}: {
  visible: boolean | undefined;
}): ReactElement | null {
  const theme = useTheme();
  if (!visible) return null;
  return (
    <View testID="discover-incomplete-results" style={styles.incompleteBanner}>
      <AlertTriangle size={14} color={theme.color.textSecondary} />
      <Text variant="caption" tone="secondary" style={styles.incompleteText}>
        {INCOMPLETE_RESULTS_MESSAGE}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  incompleteBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.sm,
  },
  incompleteText: { flex: 1 },
});
