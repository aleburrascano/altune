import type { ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { spacing, useTheme } from '@shared/ui/theme';

type AuthFullScreenNoticeProps = {
  children: ReactNode;
  padded?: boolean;
  testID?: string;
};

export function AuthFullScreenNotice({
  children,
  padded = false,
  testID,
}: AuthFullScreenNoticeProps) {
  const theme = useTheme();

  return (
    <View
      testID={testID}
      style={[
        styles.notice,
        padded ? styles.padded : null,
        { backgroundColor: theme.color.canvas },
      ]}
    >
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  notice: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md },
  padded: { padding: spacing.lg },
});
