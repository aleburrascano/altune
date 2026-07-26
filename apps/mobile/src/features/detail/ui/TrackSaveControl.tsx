import type { ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import {
  trackIdentityKey,
  useTrackIdForIdentity,
  useTrackStatus,
} from '@shared/acquisition/trackStatusStore';
import { radius, useTheme } from '@shared/ui/theme';

import { saveControlLabel, saveControlState, type SaveControlState } from '../save-control-state';

import { SaveGlyph } from './SaveGlyph';

const SIZE = 40;

export function TrackSaveControl({
  state,
  onPress,
  title,
  artist,
  testID,
}: {
  state: SaveControlState;
  onPress: () => void;
  title: string;
  artist?: string | null;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  const identity = artist != null ? trackIdentityKey(title, artist) : null;
  const linkedId = useTrackIdForIdentity(identity);
  const live = useTrackStatus(linkedId ?? null);

  const effective: SaveControlState =
    linkedId != null && live != null
      ? saveControlState({ trackId: linkedId, acquisitionStatus: live.acquisitionStatus })
      : state;
  const interactive = effective === 'add' || effective === 'failed';

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
      accessibilityLabel={saveControlLabel(effective, title)}
      hitSlop={8}
      style={({ pressed }) => [
        styles.base,
        effective === 'add' ? { borderWidth: 1.5, borderColor: theme.color.border } : null,
        effective === 'ready' ? { backgroundColor: `${theme.color.success}28` } : null,
        effective === 'failed' ? { backgroundColor: `${theme.color.danger}28` } : null,
        pressed && interactive ? { opacity: 0.6 } : null,
      ]}
    >
      <SaveGlyph state={effective} addSize={20} addTone="accent" />
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
