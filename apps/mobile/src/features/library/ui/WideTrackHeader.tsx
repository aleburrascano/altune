import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import type { StyleProp, TextStyle } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

export const WIDE_TRACK_COLUMNS = { artwork: 48, duration: 64, status: 96 };

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  title: { flex: 3 },
  artist: { flex: 2 },
  album: { flex: 2 },
  duration: { width: 64, textAlign: 'right' },
});

type Column = { key: string; label: string; style: StyleProp<TextStyle> };

const COLUMNS: Column[] = [
  { key: 'title', label: 'Title', style: styles.title },
  { key: 'artist', label: 'Artist', style: styles.artist },
  { key: 'album', label: 'Album', style: styles.album },
  { key: 'duration', label: 'Duration', style: styles.duration },
];

function HeaderLabels(): ReactElement {
  return (
    <>
      {COLUMNS.map((column) => (
        <Text key={column.key} variant="label" tone="secondary" style={column.style}>{column.label}</Text>
      ))}
    </>
  );
}

export function WideTrackHeader(): ReactElement {
  const theme = useTheme();
  return (
    <View testID="library-wide-track-header" style={[styles.row, { borderBottomColor: theme.color.border }]}>
      <View style={{ width: WIDE_TRACK_COLUMNS.artwork }} />
      <HeaderLabels />
      <View style={{ width: WIDE_TRACK_COLUMNS.status }} />
    </View>
  );
}
