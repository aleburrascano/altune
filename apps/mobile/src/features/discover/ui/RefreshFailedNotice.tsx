import type { ReactElement } from 'react';
import { StyleSheet } from 'react-native';

import { Text, spacing } from '@shared/ui';

export const REFRESH_FAILED_MESSAGE = "Couldn't refresh, showing earlier results";

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
