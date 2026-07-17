/**
 * TrackSaveControl — the 40pt circular save/download control on a track row.
 *
 * One control, the whole acquire lifecycle: add (not in library) → saving
 * (audio downloading server-side) → ready (playable) → failed (tap to retry).
 * The visible affordance is a proper tappable circle (not a bare 16pt glyph),
 * so it reads as pressable and clears the 44pt touch-target bar.
 */

import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { useTheme } from '@shared/ui/theme';
import { radius } from '@shared/ui/theme/tokens';

import { saveControlLabel, type SaveControlState } from '../save-control-state';

import { SaveGlyph } from './SaveGlyph';

const SIZE = 40;

export function TrackSaveControl({
  state,
  onPress,
  title,
  testID,
}: {
  state: SaveControlState;
  onPress: () => void;
  title: string;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  const interactive = state === 'add' || state === 'failed';

  return (
    <Pressable
      testID={testID}
      onPress={(e) => {
        e.stopPropagation();
        if (interactive) {
          onPress();
        }
      }}
      disabled={!interactive}
      accessibilityRole="button"
      accessibilityLabel={saveControlLabel(state, title)}
      hitSlop={8}
      style={({ pressed }) => [
        styles.base,
        state === 'add' ? { borderWidth: 1.5, borderColor: theme.color.border } : null,
        state === 'ready' ? { backgroundColor: `${theme.color.success}28` } : null,
        state === 'failed' ? { backgroundColor: `${theme.color.danger}28` } : null,
        pressed && interactive ? { opacity: 0.6 } : null,
      ]}
    >
      <SaveGlyph state={state} addSize={20} addTone="accent" />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: {
    width: SIZE,
    height: SIZE,
    borderRadius: radius.full,
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  },
});
