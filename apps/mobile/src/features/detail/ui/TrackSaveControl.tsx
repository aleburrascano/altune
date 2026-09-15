import { useEffect, useRef, type ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { radius, useTheme } from '@shared/ui/theme';

import { saveControlLabel, saveControlState, type SaveControlState } from '../save-control-state';
import { useResolvedOwnedTrack, type TrackIdentity } from '../hooks/useOwnedTrack';

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
  const identity: TrackIdentity | undefined = artist != null ? { title, artist } : undefined;
  const owned = useResolvedOwnedTrack(null, identity);

  const effective: SaveControlState = owned != null ? saveControlState(owned) : state;
  const interactive = effective === 'add' || effective === 'failed';

  // A quick-save mutation flushes its in-flight status through the (batched)
  // store a beat after onPress fires, so a fast double-tap can re-enter before
  // `effective` reflects the first save. This synchronous one-shot latch blocks
  // the second tap at the event, then releases when `effective` next changes
  // (the store caught up, or the save settled into a retryable failure).
  const inFlight = useRef(false);
  useEffect(() => {
    inFlight.current = false;
  }, [effective]);

  return (
    <Pressable
      testID={testID}
      onPress={(e) => {
        e.stopPropagation();
        if (!interactive || inFlight.current) return;
        inFlight.current = true;
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
