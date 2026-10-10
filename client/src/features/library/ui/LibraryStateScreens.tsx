import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { describeError } from '@shared/failure/describeError';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';

const SKELETON_TILES = ['tile-a', 'tile-b', 'tile-c', 'tile-d'];

function SkeletonGrid(): ReactElement {
  return (
    <View testID="library-loading" style={styles.skeletonGrid}>
      {SKELETON_TILES.map((id) => (
        <Skeleton key={id} width="47%" height={140} radius={8} />
      ))}
    </View>
  );
}

export function LibrarySkeletonScreen(): ReactElement {
  return (
    <Screen>
      <LibraryHeader />
      <SkeletonGrid />
    </Screen>
  );
}

type CenterProps = { testID: string; title: string; body: string; children: ReactElement };

function SubText({ body }: { body: string }): ReactElement {
  return (
    <Text variant="label" tone="secondary" style={styles.centerSub}>
      {body}
    </Text>
  );
}

function CenterBody({ title, body, children }: Omit<CenterProps, 'testID'>): ReactElement {
  return (
    <>
      <Text variant="title">{title}</Text>
      <SubText body={body} />
      {children}
    </>
  );
}

function CenterMessage({ testID, ...rest }: CenterProps): ReactElement {
  return (
    <Screen>
      <LibraryHeader />
      <View testID={testID} style={styles.center}>
        <CenterBody {...rest} />
      </View>
    </Screen>
  );
}

export function LibraryErrorScreen(props: { error: unknown; onRetry: () => void }): ReactElement {
  const { title, body } = describeError(props.error);
  return (
    <CenterMessage testID="library-error" title={title} body={body}>
      <Button action="library.retry" testID="library-retry" label="Retry" onPress={props.onRetry} />
    </CenterMessage>
  );
}

const EMPTY_BODY = 'Tracks you add will show up here.';

export function LibraryEmptyScreen({ onDiscover }: { onDiscover: () => void }): ReactElement {
  return (
    <CenterMessage testID="library-empty" title="Your library is empty" body={EMPTY_BODY}>
      <Button action="library.empty.discover" label="Discover Music" onPress={onDiscover} />
    </CenterMessage>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
  skeletonGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.md,
    paddingTop: spacing.xl,
  },
});

interface LibraryNoResultsProps {
  query: string;
  onClear: () => void;
}

export function LibraryNoResults({ query, onClear }: LibraryNoResultsProps) {
  return (
    <View testID="library-no-results" style={noResultsStyles.center}>
      <Text variant="title" style={noResultsStyles.title}>
        No results for &ldquo;{query}&rdquo;
      </Text>
      <Text variant="label" tone="secondary" style={noResultsStyles.sub}>
        Your library is still here — it&apos;s just filtered by your search.
      </Text>
      <Button
        action="library.search.clear"
        testID="library-clear-search"
        label="Clear search"
        onPress={onClear}
      />
    </View>
  );
}

const noResultsStyles = StyleSheet.create({
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  title: { textAlign: 'center' },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
});

export function LibraryHeader(): ReactElement {
  return (
    <View style={headerStyles.header}>
      <Text variant="displayL">Library</Text>
    </View>
  );
}

const headerStyles = StyleSheet.create({
  header: {
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
});
