import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { spacing, useTheme } from '@shared/ui/theme';

const STAGING_SUPABASE_PROJECT_REF = 'ijyjoyxhwmbmriwzazbx';

export const STAGING_BANNER_COPY =
  'Staging. Everything here is wiped every night. Use altune.duckdns.org.';

function isStagingBuild(): boolean {
  return (process.env.EXPO_PUBLIC_SUPABASE_URL ?? '').includes(STAGING_SUPABASE_PROJECT_REF);
}

export function StagingBanner() {
  return isStagingBuild() ? <StagingNotice /> : null;
}

function StagingNotice() {
  const { color } = useTheme();
  const banner = [styles.banner, { backgroundColor: color.surface2 }];
  const copy = (
    <Text tone="warning" style={styles.copy}>
      {STAGING_BANNER_COPY}
    </Text>
  );
  return <View style={banner}>{copy}</View>;
}

const styles = StyleSheet.create({
  banner: { paddingVertical: spacing.sm, paddingHorizontal: spacing.md },
  copy: { textAlign: 'center' },
});
