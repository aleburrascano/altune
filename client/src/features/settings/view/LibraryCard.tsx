import { Sparkles } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { Text } from '@shared/ui';
import {
  backfillActionLabel,
  backfillActionTone,
  backfillDetail,
} from '@features/settings/core/backfillStatus';
import type { RetryOptions } from '@features/settings/view/retryOptions';
import { useBackfillFeatured } from '@features/settings/view/useBackfillFeatured';
import { SettingsCard, SettingsRow } from '@features/settings/view/SettingsCard';

type Backfill = ReturnType<typeof useBackfillFeatured>;

type LibraryCardProps = { retryOptions: RetryOptions };

export function LibraryCard({ retryOptions }: LibraryCardProps): ReactElement {
  const backfill = useBackfillFeatured(retryOptions);
  return (
    <SettingsCard label="Library">
      <SettingsRow {...libraryRowProps(backfill)} />
    </SettingsCard>
  );
}

function libraryRowProps(backfill: Backfill) {
  return {
    testID: 'settings-backfill-featured',
    first: true,
    icon: Sparkles,
    tone: 'warning' as const,
    label: 'Resolve featured artists',
    detail: backfillDetail(backfill),
    onPress: () => backfill.mutate(),
    disabled: backfill.isPending,
    right: <BackfillStatusLabel backfill={backfill} />,
  };
}

function BackfillStatusLabel({ backfill }: { backfill: Backfill }): ReactElement {
  return (
    <Text variant="label" tone={backfillActionTone(backfill)}>
      {backfillActionLabel(backfill)}
    </Text>
  );
}
