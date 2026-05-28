/**
 * DiscoverRow — single search-result entry.
 *
 * Slice 46. testID = `discover-row-<signature>` where signature is the
 * server-computed result_signature (or a fallback hash of title+subtitle).
 */

import type { ReactElement } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';

import type { DiscoveryResult } from '../../../shared/api-client/discovery';

export type DiscoverRowProps = {
  result: DiscoveryResult;
  position: number;
  onPress: (result: DiscoveryResult, position: number) => void;
};

export function DiscoverRow({ result, position, onPress }: DiscoverRowProps): ReactElement {
  const testId = `discover-row-${result.kind}-${position}`;
  return (
    <TouchableOpacity
      style={styles.row}
      testID={testId}
      onPress={() => onPress(result, position)}
    >
      <View style={styles.body}>
        <Text style={styles.title} numberOfLines={1}>
          {result.title}
        </Text>
        {result.subtitle !== null && (
          <Text style={styles.subtitle} numberOfLines={1}>
            {result.subtitle}
          </Text>
        )}
        <View style={styles.metaRow}>
          <Text style={styles.kind}>{result.kind}</Text>
          <Text style={styles.confidence}>· {result.confidence}</Text>
          {result.sources.length > 1 && (
            <Text style={styles.multiSource}>· {result.sources.length} sources</Text>
          )}
        </View>
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#222',
  },
  body: { flexDirection: 'column' },
  title: { color: '#fff', fontSize: 16, fontWeight: '500' },
  subtitle: { color: '#aaa', fontSize: 13, marginTop: 2 },
  metaRow: { flexDirection: 'row', marginTop: 4 },
  kind: { color: '#888', fontSize: 11, textTransform: 'uppercase' },
  confidence: { color: '#888', fontSize: 11, marginLeft: 4 },
  multiSource: { color: '#5fa3ff', fontSize: 11, marginLeft: 4 },
});
