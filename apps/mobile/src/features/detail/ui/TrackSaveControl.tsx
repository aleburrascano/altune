/**
 * TrackSaveControl — the 40pt circular save/download control on a track row.
 *
 * One control, the whole acquire lifecycle: add (not in library) → saving
 * (audio downloading server-side) → ready (playable) → failed (tap to retry).
 * The visible affordance is a proper tappable circle (not a bare 16pt glyph),
 * so it reads as pressable and clears the 44pt touch-target bar.
 */

import type { ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet } from 'react-native';

import { Check, Plus, RotateCw } from 'lucide-react-native';

import { useTheme } from '@shared/ui/theme';
import { radius } from '@shared/ui/theme/tokens';

import type { SaveControlState } from '../save-control-state';

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

  const label =
    state === 'add'
      ? `Save ${title}`
      : state === 'saving'
        ? `${title} downloading`
        : state === 'ready'
          ? `${title} in library`
          : `Retry saving ${title}`;

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
      accessibilityLabel={label}
      hitSlop={8}
      style={({ pressed }) => [
        styles.base,
        state === 'add' ? { borderWidth: 1.5, borderColor: theme.color.border } : null,
        state === 'ready' ? { backgroundColor: `${theme.color.success}28` } : null,
        state === 'failed' ? { backgroundColor: `${theme.color.danger}28` } : null,
        pressed && interactive ? { opacity: 0.6 } : null,
      ]}
    >
      {state === 'saving' ? (
        <ActivityIndicator size="small" color={theme.color.accent} />
      ) : state === 'ready' ? (
        <Check size={18} color={theme.color.success} />
      ) : state === 'failed' ? (
        <RotateCw size={17} color={theme.color.danger} />
      ) : (
        <Plus size={20} color={theme.color.accent} />
      )}
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
