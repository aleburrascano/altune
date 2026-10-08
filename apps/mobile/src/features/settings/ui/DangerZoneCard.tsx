import { Eraser, LogOut, Trash2, type LucideIcon } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';

import { Text } from '@shared/ui';
import { ConfirmModal } from '@features/settings/ui/ConfirmModal';
import {
  buildDangerZoneActions,
  type DangerZoneActionKey,
} from '@features/settings/domain/dangerZoneActions';
import { SettingsCard } from '@features/settings/ui/SettingsCard';
import { SettingsRow } from '@features/settings/ui/SettingsRow';

const DANGER_ZONE_ICONS: Record<DangerZoneActionKey, LucideIcon> = {
  downloads: Trash2,
  history: Eraser,
  'sign-out': LogOut,
};

type DangerZoneCardProps = Parameters<typeof buildDangerZoneActions>[0];
type Action = ReturnType<typeof buildDangerZoneActions>[number];

function DangerZoneRow(props: {
  action: Action;
  first: boolean;
  onPress: () => void;
}): ReactElement {
  const { action, first, onPress } = props;
  const { key, row } = action;
  return (
    <SettingsRow
      testID={row.testID}
      first={first}
      icon={DANGER_ZONE_ICONS[key]}
      tone="danger"
      label={row.label}
      detail={row.detail}
      onPress={onPress}
      disabled={row.disabled ?? false}
      right={
        row.status ? (
          <Text variant="label" tone={row.status.tone}>
            {row.status.label}
          </Text>
        ) : null
      }
    />
  );
}

function DangerZoneConfirm(props: {
  action: Action;
  visible: boolean;
  onClose: () => void;
}): ReactElement {
  const { action, visible, onClose } = props;
  const { key, confirm } = action;
  return (
    <ConfirmModal
      testID={confirm.testID}
      visible={visible}
      icon={DANGER_ZONE_ICONS[key]}
      title={confirm.title}
      body={confirm.body}
      confirmLabel={confirm.confirmLabel}
      onConfirm={confirm.onConfirm}
      onClose={onClose}
    />
  );
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
