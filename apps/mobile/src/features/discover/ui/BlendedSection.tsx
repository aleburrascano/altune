import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { ChevronRight } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import { DiscoverRow } from './DiscoverRow';
import { ResultsList, type ResultsCommonProps } from './ResultsList';
import { TopResultCard } from './TopResultCard';
import { kindLabel, resultKey } from '../state';

import type { DiscoveryKind, DiscoveryResult, ResultSection } from '@shared/api-client/discovery';

export function BlendedSection({
  sections,
  topResult,
  onSeeAll,
  common,
}: {
  sections: ResultSection[];
  topResult: DiscoveryResult | undefined;
  onSeeAll: (filter: DiscoveryKind) => void;
  common: ResultsCommonProps;
}): ReactElement {
  const theme = useTheme();
  const visible = sections.filter((section) => section.items.length > 0);

  return (
    <ResultsList
      data={visible}
      keyExtractor={(section) => section.kind}
      headerExtra={
        topResult !== undefined ? (
          <TopResultCard result={topResult} onPress={common.onResultTap} />
        ) : null
      }
      common={common}
      renderItem={({ item: section }) => {
        const title = kindLabel(section.kind, { plural: true });
        return (
          <View style={styles.section}>
            <Text variant="label" tone="tertiary" style={styles.sectionHeader}>
              {title.toUpperCase()}
            </Text>
            {section.items.map((result, index) => (
              <DiscoverRow
                key={resultKey(result, index)}
                result={result}
                position={index}
                onPress={common.onResultTap}
              />
            ))}
            {section.has_more ? (
              <Pressable
                testID={`discover-see-all-${section.kind}`}
                onPress={() => onSeeAll(section.kind)}
                accessibilityRole="button"
                accessibilityLabel={`See all ${title.toLowerCase()}`}
                hitSlop={8}
                style={({ pressed }) => [styles.seeAll, pressed ? { opacity: 0.7 } : null]}
              >
                <Text variant="label" tone="accent">
                  See all {title.toLowerCase()}
                </Text>
                <ChevronRight size={16} color={theme.color.accent} />
              </Pressable>
            ) : null}
          </View>
        );
      }}
    />
  );
}

const styles = StyleSheet.create({
  sectionHeader: { marginBottom: spacing.sm, marginTop: spacing.sm, letterSpacing: 1 },
  section: { marginBottom: spacing.xl },
  seeAll: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.md,
    alignSelf: 'flex-start',
    minHeight: 44,
  },
});
