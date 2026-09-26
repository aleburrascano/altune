import { type ReactElement } from 'react';

import { Plus } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme';

import { SavePillShell } from './SavePillShell';

type SaveAllPillProps = { unownedCount: number; saving: boolean; onSave: () => void };

function saveAllShellProps(props: SaveAllPillProps) {
  return {
    testID: 'detail-save-all',
    onPress: props.onSave,
    disabled: props.saving,
    interactive: !props.saving,
    accessibilityLabel: `Save ${props.unownedCount} tracks to your library`,
    accessibilityState: { disabled: props.saving },
  };
}

export function SaveAllPill(props: SaveAllPillProps): ReactElement | null {
  const theme = useTheme();
  if (props.unownedCount === 0) return null;
  return (
    <SavePillShell {...saveAllShellProps(props)}>
      <Plus size={18} color={theme.color.accent} />
      <Text variant="label">{props.saving ? 'Saving…' : `Save ${props.unownedCount}`}</Text>
    </SavePillShell>
  );
}
