import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { AlertTriangle } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';

const REFRESH_FAILED_MESSAGE = "Couldn't refresh, showing earlier results";

function NoticeText(): ReactElement {
  return (
    <Text testID="discover-refresh-failed" variant="caption" tone="secondary" style={styles.notice}>
      {REFRESH_FAILED_MESSAGE}
    </Text>
  );
}

export function RefreshFailedNotice({ visible }: { visible: boolean | undefined }) {
  if (!visible) return null;
  return <NoticeText />;
}

const styles = StyleSheet.create({
  notice: { paddingBottom: spacing.sm },
});

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
    <View testID="discover-incomplete-results" style={bannerStyles.incompleteBanner}>
      <AlertTriangle size={14} color={theme.color.textSecondary} />
      <Text variant="caption" tone="secondary" style={bannerStyles.incompleteText}>
        {INCOMPLETE_RESULTS_MESSAGE}
      </Text>
    </View>
  );
}

const bannerStyles = StyleSheet.create({
  incompleteBanner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.sm,
  },
  incompleteText: { flex: 1 },
});
