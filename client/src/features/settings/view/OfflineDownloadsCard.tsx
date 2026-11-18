import { DownloadCloud } from 'lucide-react-native';
import type { ReactElement } from 'react';

import type { DownloadStats } from '@features/settings/core/downloadStatsModel';
import { SettingsCard, SettingsRow } from '@features/settings/view/SettingsCard';

type OfflineDownloadsCardProps = {
  stats: DownloadStats;
};

export function OfflineDownloadsCard({ stats }: OfflineDownloadsCardProps): ReactElement {
  return (
    <SettingsCard label="Offline downloads">
      <SettingsRow {...downloadsRowProps(stats)} />
    </SettingsCard>
  );
}

function downloadsRowProps(stats: DownloadStats) {
  return {
    testID: 'settings-downloads-usage',
    first: true,
    icon: DownloadCloud,
    tone: stats.downloadCount > 0 ? ('success' as const) : ('neutral' as const),
    label: stats.usageLabel,
    detail: stats.usageDetail,
  };
}
