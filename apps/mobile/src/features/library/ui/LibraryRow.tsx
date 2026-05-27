/**
 * Single track row in the library list.
 *
 * Title (primary) + artist (secondary), per the spec's AC#1. Album, duration,
 * and album art are deliberately absent in v1 — they earn their place in
 * future feature specs.
 */

import { StyleSheet, Text, View } from 'react-native';

import type { TrackResponse } from '../../../shared/api-client/types';

export function LibraryRow({ track }: { track: TrackResponse }): JSX.Element {
  return (
    <View style={styles.row} testID={`library-row-${track.id}`}>
      <Text style={styles.title} numberOfLines={1}>
        {track.title}
      </Text>
      <Text style={styles.artist} numberOfLines={1}>
        {track.artist}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: 12,
    paddingHorizontal: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#222',
  },
  title: {
    color: '#fff',
    fontSize: 16,
    fontWeight: '500',
    marginBottom: 2,
  },
  artist: {
    color: '#888',
    fontSize: 13,
  },
});
