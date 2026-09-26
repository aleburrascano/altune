import { Sparkles } from 'lucide-react-native';
import type { ReactElement } from 'react';

import { Text } from '@shared/ui';
import { backfillActionLabel, backfillActionTone, backfillDetail } from '../hooks/backfillStatus';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';

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
    testID: 'settings-backfill-featured', first: true, icon: Sparkles,
    tone: 'warning' as const, label: 'Resolve featured artists',
    detail: backfillDetail(backfill), onPress: () => backfill.mutate(),
    disabled: backfill.isPending, right: <BackfillStatusLabel backfill={backfill} />,
  };
}

function BackfillStatusLabel({ backfill }: { backfill: Backfill }): ReactElement {
  return (
    <Text variant="label" tone={backfillActionTone(backfill)}>
      {backfillActionLabel(backfill)}
    </Text>
  );
}
