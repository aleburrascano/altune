import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Search } from 'lucide-react-native';

import { Chip, Text, spacing, useTheme } from '@shared/ui';

import { SectionLabel } from './SectionLabel';
import { pressedStyle } from './pressedStyle';
import type { SearchHistoryItem } from '@shared/api-client/discovery';

const HISTORY_LABEL_MAX_CHARS = 40;

interface RecentSearchesProps {
  historyItems: SearchHistoryItem[];
  onHistoryTap: (item: SearchHistoryItem) => void;
  onClearHistory: () => void;
  clearHistoryFailed: boolean | undefined;
}

function historyLabel(query: string): string {
  if (query.length <= HISTORY_LABEL_MAX_CHARS) return query;
  return `${query.slice(0, HISTORY_LABEL_MAX_CHARS)}…`;
}

function NoHistory(): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.emptyCenter}>
      <Search size={32} color={theme.color.textTertiary} />
      <Text variant="body" tone="secondary" style={styles.emptyText}>
        Search music to get started.
      </Text>
    </View>
  );
}

function HistoryHeader({ onClear }: { onClear: () => void }): ReactElement {
  return (
    <View style={styles.header}>
      <SectionLabel>RECENT SEARCHES</SectionLabel>
      <Pressable
        onPress={onClear}
        accessibilityRole="button"
        accessibilityLabel="Clear search history"
        hitSlop={8}
        style={({ pressed }) => pressedStyle(pressed)}
      >
        <Text variant="caption" tone="accent">
          Clear
        </Text>
      </Pressable>
    </View>
  );
}

function ClearFailed(): ReactElement {
  return (
    <Text testID="discover-clear-history-error" variant="caption" tone="secondary">
      Couldn't clear history. Try again.
    </Text>
  );
}

function HistoryChips(props: RecentSearchesProps): ReactElement {
  return (
    <View style={styles.chipCloud}>
      {props.historyItems.map((item, index) => (
        <Chip
          key={item.query_norm}
          testID={`discover-history-row-${index}`}
          label={historyLabel(item.query)}
          onPress={() => props.onHistoryTap(item)}
        />
      ))}
    </View>
  );
}

export function RecentSearches(props: RecentSearchesProps): ReactElement {
  if (props.historyItems.length === 0) return <NoHistory />;
  return (
    <>
      <HistoryHeader onClear={props.onClearHistory} />
      {props.clearHistoryFailed === true ? <ClearFailed /> : null}
      <HistoryChips {...props} />
    </>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: spacing.md,
  },
  chipCloud: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  emptyCenter: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.md },
  emptyText: { textAlign: 'center' },
});
