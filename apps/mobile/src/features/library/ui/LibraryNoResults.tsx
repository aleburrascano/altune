import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

interface LibraryNoResultsProps {
  query: string;
  onClear: () => void;
}

export function LibraryNoResults({ query, onClear }: LibraryNoResultsProps) {
  return (
    <View testID="library-no-results" style={styles.center}>
      <Text variant="title" style={styles.title}>
        No results for &ldquo;{query}&rdquo;
      </Text>
      <Text variant="label" tone="secondary" style={styles.sub}>
        Your library is still here — it&apos;s just filtered by your search.
      </Text>
      <Button testID="library-clear-search" label="Clear search" onPress={onClear} />
    </View>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  title: { textAlign: 'center' },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
});
