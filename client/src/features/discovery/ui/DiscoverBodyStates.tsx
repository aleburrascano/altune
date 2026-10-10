import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, Skeleton, Text, radius, spacing } from '@shared/ui';

import { describeError } from '@shared/failure/describeError';
import { SEARCH_UNAVAILABLE_TITLE } from '@features/discovery/domain/discoverView';
import { ART_SIZE } from '@features/discovery/ui/DiscoverRow';
import { FilterChips } from '@features/discovery/ui/FilterChips';
import { IncompleteResultsBanner } from '@features/discovery/ui/ResultNotices';
import type { ResultsFilter } from '@features/discovery/hooks/useResultsFilter';

const SKELETON_ROWS = ['row-a', 'row-b', 'row-c', 'row-d', 'row-e', 'row-f'];

function SkeletonText(): ReactElement {
  return (
    <View style={skeletonStyles.text}>
      <Skeleton width="70%" height={14} />
      <Skeleton width="40%" height={12} />
    </View>
  );
}

function SkeletonRow(): ReactElement {
  return (
    <View style={skeletonStyles.row}>
      <Skeleton width={ART_SIZE} height={ART_SIZE} radius={radius.md} />
      <SkeletonText />
    </View>
  );
}

export function DiscoverSkeleton(): ReactElement {
  return (
    <View testID="discover-loading" style={skeletonStyles.list}>
      {SKELETON_ROWS.map((id) => (
        <SkeletonRow key={id} />
      ))}
    </View>
  );
}

const skeletonStyles = StyleSheet.create({
  list: { flex: 1, paddingTop: spacing.sm },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    paddingHorizontal: spacing.xs,
  },
  text: { flex: 1, gap: spacing.sm },
});

export function DiscoverUnavailable(): ReactElement {
  return (
    <View testID="discover-unavailable" style={errorStyles.center}>
      <Text variant="title">{SEARCH_UNAVAILABLE_TITLE}</Text>
      <Text variant="label" tone="secondary" style={errorStyles.sub}>
        Check back in a little while.
      </Text>
    </View>
  );
}

interface FullErrorProps {
  error: unknown;
  onRetry: () => void;
}

function ErrorCopy({ title, body }: { title: string; body: string }): ReactElement {
  return (
    <>
      <Text variant="title">{title}</Text>
      <Text variant="label" tone="secondary" style={errorStyles.sub}>
        {body}
      </Text>
    </>
  );
}

export function DiscoverFullError({ error, onRetry }: FullErrorProps): ReactElement {
  const copy = describeError(error);
  return (
    <View testID="discover-full-error" style={errorStyles.center}>
      <ErrorCopy {...copy} />
      <Button
        action="discover.error.retry"
        testID="discover-retry"
        label="Retry"
        onPress={onRetry}
      />
    </View>
  );
}

const errorStyles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});

interface ZeroResultsProps {
  filter: ResultsFilter;
  onFilterChange: (filter: ResultsFilter) => void;
  resultsIncomplete?: boolean | undefined;
}

function NoMatches(): ReactElement {
  return (
    <View style={zeroStyles.center}>
      <Text variant="title">No matches</Text>
      <Text variant="label" tone="secondary" style={zeroStyles.sub}>
        Check spelling or try fewer words.
      </Text>
    </View>
  );
}

export function DiscoverZeroResults(props: ZeroResultsProps): ReactElement {
  return (
    <View testID="discover-zero-results" style={zeroStyles.root}>
      <FilterChips active={props.filter} onSelect={props.onFilterChange} />
      <IncompleteResultsBanner visible={props.resultsIncomplete} />
      <NoMatches />
    </View>
  );
}

const zeroStyles = StyleSheet.create({
  root: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});
