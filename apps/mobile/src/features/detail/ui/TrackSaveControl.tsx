import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { radius, useTheme } from '@shared/ui/theme';

import {
  saveControlInteractive,
  saveControlLabel,
  saveControlState,
  type SaveControlState,
} from '../save-control-state';
import { useResolvedOwnedTrack, type OwnedTrack, type TrackIdentity } from '../hooks/useOwnedTrack';

import { sharedStyles } from './styles';
import { SaveGlyph } from './SaveGlyph';

const SIZE = 40;

export function TrackSaveControl({
  owned,
  onPress,
  title,
  artist,
  savingInBatch = false,
  testID,
}: {
  owned: OwnedTrack | null;
  onPress: () => void;
  title: string;
  artist?: string | null;
  savingInBatch?: boolean;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  const identity: TrackIdentity | undefined = artist != null ? { title, artist } : undefined;
  const resolved = useResolvedOwnedTrack(owned, identity);

  const ownState: SaveControlState = saveControlState(resolved);
  const effective: SaveControlState = savingInBatch && ownState === 'add' ? 'saving' : ownState;
  const interactive = !savingInBatch && saveControlInteractive(effective);

  return (
    <Pressable
      testID={testID}
      onPress={(e) => {
        e.stopPropagation();
        if (!interactive) return;
        onPress();
      }}
      disabled={!interactive}
      accessibilityRole="button"
      accessibilityLabel={saveControlLabel(effective, title)}
      hitSlop={8}
      style={({ pressed }) => [
        styles.base,
        effective === 'add' ? { borderWidth: 1.5, borderColor: theme.color.border } : null,
        effective === 'ready' ? { backgroundColor: `${theme.color.success}28` } : null,
        effective === 'failed' ? { backgroundColor: `${theme.color.danger}28` } : null,
        pressed && interactive ? sharedStyles.pressed : null,
      ]}
    >
      <SaveGlyph state={effective} addSize={20} />
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
