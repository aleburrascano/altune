import type { ReactElement, ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { Tappable, Row, useTheme, Artwork, halved } from '@shared/ui';
import { Check } from 'lucide-react-native';
import { rowSurfaceStyle } from '@features/library/view/libraryRowStyle';
import type { TrackResponse } from '@shared/catalog';

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
    <Tappable
      action="library.row.toggleSelect"
      trackId={track.id}
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
    </Tappable>
  );
}

const CHECKBOX_SIZE = 22;
const CHECKBOX_BORDER_WIDTH = 1.5;

const styles = StyleSheet.create({
  checkbox: {
    width: CHECKBOX_SIZE,
    height: CHECKBOX_SIZE,
    borderRadius: halved(CHECKBOX_SIZE),
    borderWidth: CHECKBOX_BORDER_WIDTH,
    alignItems: 'center',
    justifyContent: 'center',
  },
});
