import { StyleSheet, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';
const STAGING_SUPABASE_PROJECT_REF = 'ijyjoyxhwmbmriwzazbx';

const STAGING_BANNER_COPY =
  'Staging. Everything here is wiped every night. Use altune.duckdns.org.';

function isStagingBuild(supabaseUrl: string | undefined): boolean {
  return (supabaseUrl ?? '').includes(STAGING_SUPABASE_PROJECT_REF);
}

export type StagingBannerProps = { supabaseUrl: string | undefined };

export function StagingBanner({ supabaseUrl }: StagingBannerProps) {
  return isStagingBuild(supabaseUrl) ? <StagingNotice /> : null;
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
