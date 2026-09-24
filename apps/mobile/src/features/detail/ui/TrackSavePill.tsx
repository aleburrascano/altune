import { type ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

import type { TrackDetailActions } from '../hooks/useTrackDetailActions';
import { saveControlLabel, saveControlText } from '../save-control-state';

import { sharedStyles } from './styles';
import { SaveGlyph } from './SaveGlyph';

export function TrackSavePill({
  actions,
  title,
}: {
  actions: TrackDetailActions;
  title: string;
}): ReactElement {
  const theme = useTheme();
  const { saveInteractive, saveState, saveDisplayState } = actions;
  return (
    <Pressable
      testID="detail-save"
      onPress={actions.onSave}
      disabled={!saveInteractive}
      accessibilityRole="button"
      accessibilityLabel={saveControlLabel(saveDisplayState, title)}
      accessibilityState={{ disabled: !saveInteractive, busy: saveState === 'saving' }}
      style={({ pressed }) => [
        styles.savePill,
        { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
        pressed && saveInteractive ? sharedStyles.pressed : null,
      ]}
    >
      <SaveGlyph state={saveDisplayState} addSize={18} />
      <Text variant="label" tone={saveState === 'ready' ? 'success' : 'primary'}>
        {saveControlText(saveDisplayState)}
      </Text>
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
