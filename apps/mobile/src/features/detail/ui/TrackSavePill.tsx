import { type ReactElement } from 'react';

import { Text } from '@shared/ui/primitives/Text';

import type { SaveState } from '@/features/detail/domain/save-control-state';
import {
  saveControlInteractive,
  saveControlLabel,
  saveControlText,
  saveDisplayState,
} from '@/features/detail/domain/save-control-state';

import { SaveGlyph } from '@features/detail/ui/SaveGlyph';
import { SavePillShell } from '@features/detail/ui/SavePillShell';

type TrackSavePillProps = { save: SaveState; onSave: () => void; title: string };

function savePillA11y({ save, title }: TrackSavePillProps) {
  const interactive = saveControlInteractive(save);
  return {
    disabled: !interactive,
    accessibilityLabel: saveControlLabel(saveDisplayState(save), title),
    accessibilityState: { disabled: !interactive, busy: save === 'saving' },
  };
}

function savePillShellProps(props: TrackSavePillProps) {
  return {
    testID: 'detail-save',
    onPress: props.onSave,
    interactive: saveControlInteractive(props.save),
    ...savePillA11y(props),
  };
}

function saveTextProps(save: SaveState) {
  return {
    variant: 'label' as const,
    tone: save === 'ready' ? 'success' : 'primary',
  } as const;
}

export function TrackSavePill(props: TrackSavePillProps): ReactElement {
  const display = saveDisplayState(props.save);
  return (
    <SavePillShell {...savePillShellProps(props)}>
      <SaveGlyph state={display} addSize={18} />
      <Text {...saveTextProps(props.save)}>{saveControlText(display)}</Text>
    </SavePillShell>
  );
}
