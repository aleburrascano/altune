import { Sparkles } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { Text } from '@shared/ui';
import {
  backfillActionLabel,
  backfillActionTone,
  backfillDetail,
} from '@features/settings/backfillStatus';
import { useBackfillFeatured } from '@features/settings/hooks/useBackfillFeatured';
import { SettingsCard } from '@features/settings/ui/SettingsCard';
import { SettingsRow } from '@features/settings/ui/SettingsRow';

type Backfill = ReturnType<typeof useBackfillFeatured>;

export function LibraryCard(): ReactElement {
  const backfill = useBackfillFeatured();
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
