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
  clearHistoryFailed?: boolean | undefined;
}

interface ChipProps {
  item: SearchHistoryItem;
  index: number;
  onTap: (item: SearchHistoryItem) => void;
}

function historyLabel(query: string): string {
  if (query.length <= HISTORY_LABEL_MAX_CHARS) return query;
  return `${query.slice(0, HISTORY_LABEL_MAX_CHARS)}…`;
}

function SearchGlyph(): ReactElement {
  const theme = useTheme();
  return <Search size={32} color={theme.color.textTertiary} />;
}

function NoHistory(): ReactElement {
  return (
    <View style={styles.emptyCenter}>
      <SearchGlyph />
      <Text variant="body" tone="secondary" style={styles.emptyText}>
        Search music to get started.
      </Text>
    </View>
  );
}

const clearStyle = ({ pressed }: { pressed: boolean }) => pressedStyle(pressed);
const CLEAR_A11Y = {
  accessibilityRole: 'button',
  accessibilityLabel: 'Clear search history',
  hitSlop: 8,
} as const;

function ClearButton({ onPress }: { onPress: () => void }): ReactElement {
  return (
    <Pressable onPress={onPress} style={clearStyle} {...CLEAR_A11Y}>
      <Text variant="caption" tone="accent">
        Clear
      </Text>
    </Pressable>
  );
}

function HistoryHeader({ onClear }: { onClear: () => void }): ReactElement {
  return (
    <View style={styles.header}>
      <SectionLabel>RECENT SEARCHES</SectionLabel>
      <ClearButton onPress={onClear} />
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

function HistoryChip({ item, index, onTap }: ChipProps): ReactElement {
  return (
    <Chip
      testID={`discover-history-row-${index}`}
      label={historyLabel(item.query)}
      onPress={() => onTap(item)}
    />
  );
}

function HistoryChips(props: RecentSearchesProps): ReactElement {
  return (
    <View style={styles.chipCloud}>
      {props.historyItems.map((item, index) => (
        <HistoryChip key={item.query_norm} item={item} index={index} onTap={props.onHistoryTap} />
      ))}
    </View>
  );
}

function HistoryContent(props: RecentSearchesProps): ReactElement {
  if (props.historyItems.length === 0) return <NoHistory />;
  return (
    <>
      <HistoryHeader onClear={props.onClearHistory} />
      {props.clearHistoryFailed === true ? <ClearFailed /> : null}
      <HistoryChips {...props} />
    </>
  );
}

export function RecentSearches(props: RecentSearchesProps): ReactElement {
  return (
    <View testID="discover-empty-no-query" style={styles.list}>
      <HistoryContent {...props} />
    </View>
  );
}

const styles = StyleSheet.create({
  list: { flex: 1, paddingTop: spacing.sm },
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
