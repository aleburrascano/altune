import { type ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme, type Theme } from '@shared/ui/theme';

import type { SaveState } from '../save-control-state';
import { saveControlInteractive, saveControlLabel, saveControlText, saveDisplayState } from '../save-control-state';

import { sharedStyles } from './styles';
import { SaveGlyph } from './SaveGlyph';

type TrackSavePillProps = { save: SaveState; onSave: () => void; title: string };

function savePillStyle(theme: Theme, interactive: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.savePill,
    { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
    pressed && interactive ? sharedStyles.pressed : null,
  ];
}

function savePillA11y({ save, title }: TrackSavePillProps) {
  const interactive = saveControlInteractive(save);
  return {
    disabled: !interactive,
    accessibilityLabel: saveControlLabel(saveDisplayState(save), title),
    accessibilityState: { disabled: !interactive, busy: save === 'saving' },
  };
}

function savePillProps(props: TrackSavePillProps, theme: Theme) {
  return {
    testID: 'detail-save',
    onPress: props.onSave,
    accessibilityRole: 'button' as const,
    style: savePillStyle(theme, saveControlInteractive(props.save)),
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
  const theme = useTheme();
  const display = saveDisplayState(props.save);
  return (
    <Pressable {...savePillProps(props, theme)}>
      <SaveGlyph state={display} addSize={18} />
      <Text {...saveTextProps(props.save)}>{saveControlText(display)}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  savePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.full,
    flexShrink: 0,
  },
});
