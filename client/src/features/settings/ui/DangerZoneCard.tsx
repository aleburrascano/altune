import { Eraser, LogOut, Trash2, type LucideIcon } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';

import { Text } from '@shared/ui';
import { ConfirmModal } from '@features/settings/ui/ConfirmModal';
import {
  buildDangerZoneActions,
  type DangerZoneActionKey,
} from '@features/settings/domain/dangerZoneActions';
import { SettingsCard, SettingsRow } from '@features/settings/ui/SettingsCard';

const DANGER_ZONE_ICONS: Record<DangerZoneActionKey, LucideIcon> = {
  downloads: Trash2,
  history: Eraser,
  'sign-out': LogOut,
};

type DangerZoneCardProps = Parameters<typeof buildDangerZoneActions>[0];
type Action = ReturnType<typeof buildDangerZoneActions>[number];

type DangerZoneRowProps = { action: Action; first: boolean; onPress: () => void };
type DangerZoneConfirmProps = { action: Action; visible: boolean; onClose: () => void };

function statusLabel({ status }: Action['row']): ReactElement | null {
  if (!status) return null;
  return (
    <Text variant="label" tone={status.tone}>
      {status.label}
    </Text>
  );
}

function rowProps(row: Action['row']) {
  return {
    testID: row.testID,
    label: row.label,
    detail: row.detail,
    disabled: row.disabled ?? false,
    tone: 'danger' as const,
    right: statusLabel(row),
  };
}

function DangerZoneRow({ action, first, onPress }: DangerZoneRowProps): ReactElement {
  const icon = DANGER_ZONE_ICONS[action.key];
  return <SettingsRow {...rowProps(action.row)} first={first} icon={icon} onPress={onPress} />;
}

function DangerZoneConfirm({ action, visible, onClose }: DangerZoneConfirmProps): ReactElement {
  const icon = DANGER_ZONE_ICONS[action.key];
  return <ConfirmModal {...action.confirm} visible={visible} icon={icon} onClose={onClose} />;
}

export function DangerZoneCard(props: DangerZoneCardProps): ReactElement {
  const [confirming, setConfirming] = useState<DangerZoneActionKey | null>(null);
  const actions = buildDangerZoneActions(props);
  const visibleRows = actions.filter(({ row }) => !row.hidden);
  const close = (): void => setConfirming(null);

  return (
    <>
      <SettingsCard label="Danger zone" danger>
        {visibleRows.map((action, index) => (
          <DangerZoneRow
            key={action.key}
            action={action}
            first={index === 0}
            onPress={() => setConfirming(action.key)}
          />
        ))}
      </SettingsCard>

      {actions.map((action) => (
        <DangerZoneConfirm
          key={action.key}
          action={action}
          visible={confirming === action.key}
          onClose={close}
        />
      ))}
    </>
  );
}
