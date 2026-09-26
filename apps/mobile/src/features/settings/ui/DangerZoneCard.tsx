import { useState, type ReactElement } from 'react';

import { Text } from '@shared/ui';
import { ConfirmModal } from './ConfirmModal';
import { buildDangerZoneActions, type DangerZoneActionKey } from '../dangerZoneActions';
import { SettingsCard } from './SettingsCard';
import { SettingsRow } from './SettingsRow';

type DangerZoneCardProps = Parameters<typeof buildDangerZoneActions>[0];

export function DangerZoneCard(props: DangerZoneCardProps): ReactElement {
  const [confirming, setConfirming] = useState<DangerZoneActionKey | null>(null);
  const actions = buildDangerZoneActions(props);
  // `first` is positional among the rows actually shown.
  const visibleRows = actions.filter(({ row }) => !row.hidden);
  const close = (): void => setConfirming(null);

  return (
    <>
      <SettingsCard label="Danger zone" danger>
        {visibleRows.map(({ key, icon, row }, index) => (
          <SettingsRow
            key={key}
            testID={row.testID}
            first={index === 0}
            icon={icon}
            tone="danger"
            label={row.label}
            detail={row.detail}
            onPress={() => setConfirming(key)}
            disabled={row.disabled ?? false}
            right={
              row.status ? (
                <Text variant="label" tone={row.status.tone}>
                  {row.status.label}
                </Text>
              ) : null
            }
          />
        ))}
      </SettingsCard>

      {actions.map(({ key, icon, confirm }) => (
        <ConfirmModal
          key={key}
          testID={confirm.testID}
          visible={confirming === key}
          icon={icon}
          title={confirm.title}
          body={confirm.body}
          confirmLabel={confirm.confirmLabel}
          onConfirm={confirm.onConfirm}
          onClose={close}
        />
      ))}
    </>
  );
}
