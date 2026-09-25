import { useEffect, useRef, type ReactElement } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { radius, useTheme } from '@shared/ui/theme';

import { saveControlLabel, saveControlState, type SaveControlState } from '../save-control-state';
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
  // The row's own owning extras (its stamped trackId + status), or null when the
  // row was never saved. Passed straight into the shared owned-track rule so this
  // control resolves the same answer as the detail rows — see useResolvedOwnedTrack.
  owned: OwnedTrack | null;
  onPress: () => void;
  title: string;
  artist?: string | null;
  // A "Save all" run already owns this track's write, dispatched or still queued.
  savingInBatch?: boolean;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  const identity: TrackIdentity | undefined = artist != null ? { title, artist } : undefined;
  const resolved = useResolvedOwnedTrack(owned, identity);

  // A batch-claimed track is being saved by someone else, so this control must not
  // offer a second write (#1658). Until the batch reaches the track its own state is
  // still 'add'; reporting 'saving' there says what is true instead of inviting a
  // duplicate tap, while a claimed track that already landed keeps its own state.
  const ownState: SaveControlState = saveControlState(resolved);
  const effective: SaveControlState = savingInBatch && ownState === 'add' ? 'saving' : ownState;
  const interactive = !savingInBatch && (effective === 'add' || effective === 'failed');

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
