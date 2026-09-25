import { type ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme, type Theme } from '@shared/ui/theme';

import type { TrackDetailActions } from '../hooks/useTrackDetailActions';
import { saveControlLabel, saveControlText } from '../save-control-state';

import { sharedStyles } from './styles';
import { SaveGlyph } from './SaveGlyph';

type TrackSavePillProps = { actions: TrackDetailActions; title: string };

function savePillStyle(theme: Theme, saveInteractive: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.savePill,
    { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
    pressed && saveInteractive ? sharedStyles.pressed : null,
  ];
}

function savePillA11y({ actions, title }: TrackSavePillProps) {
  const { saveInteractive, saveState, saveDisplayState } = actions;
  return {
    disabled: !saveInteractive,
    accessibilityLabel: saveControlLabel(saveDisplayState, title),
    accessibilityState: { disabled: !saveInteractive, busy: saveState === 'saving' },
  };
}

function savePillProps(props: TrackSavePillProps, theme: Theme) {
  return {
    testID: 'detail-save',
    onPress: props.actions.onSave,
    accessibilityRole: 'button' as const,
    style: savePillStyle(theme, props.actions.saveInteractive),
    ...savePillA11y(props),
  };
}

function saveTextProps(saveState: TrackDetailActions['saveState']) {
  return {
    variant: 'label' as const,
    tone: saveState === 'ready' ? 'success' : 'primary',
  } as const;
}

export function TrackSavePill(props: TrackSavePillProps): ReactElement {
  const theme = useTheme();
  const { saveState, saveDisplayState } = props.actions;
  return (
    <Pressable {...savePillProps(props, theme)}>
      <SaveGlyph state={saveDisplayState} addSize={18} />
      <Text {...saveTextProps(saveState)}>{saveControlText(saveDisplayState)}</Text>
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
