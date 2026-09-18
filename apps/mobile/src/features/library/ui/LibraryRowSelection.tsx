import type { ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Check } from 'lucide-react-native';

import { Row, useTheme } from '@shared/ui';
import { Artwork } from '@shared/ui/primitives/Artwork';

import { rowSurfaceStyle } from './libraryRowSurface';

import type { TrackResponse } from '@shared/api-client/types';

/** The library row in selection mode: pressing it checks the track rather than playing it. */
export function LibraryRowSelection({
  track,
  a11yLabel,
  selected,
  onToggle,
  onLongPress,
  children,
}: {
  track: TrackResponse;
  a11yLabel: string;
  selected: boolean;
  onToggle: () => void;
  onLongPress: (() => void) | undefined;
  children: ReactNode;
}): ReactElement {
  const theme = useTheme();
  const highlight = selected ? { backgroundColor: `${theme.color.accent}1A` } : null;
  return (
    <Pressable
      testID={`library-row-${track.id}`}
      onPress={onToggle}
      {...(onLongPress != null ? { onLongPress } : {})}
      accessibilityRole="checkbox"
      accessibilityLabel={a11yLabel}
      accessibilityState={{ checked: selected }}
      style={rowSurfaceStyle(theme, highlight)}
    >
      <Row
        leading={
          <Artwork uri={track.artwork_url} size={48} radius={6} accessibilityLabel="Album art" />
        }
        trailing={
          <View
            testID={`library-row-check-${track.id}`}
            style={[
              styles.checkbox,
              selected
                ? { backgroundColor: theme.color.accent, borderColor: theme.color.accent }
                : { borderColor: theme.color.border },
            ]}
          >
            {selected ? <Check size={14} color={theme.color.onAccent} /> : null}
          </View>
        }
      >
        {children}
      </Row>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  checkbox: {
    width: 22,
    height: 22,
    borderRadius: 11,
    borderWidth: 1.5,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
