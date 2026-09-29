import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { describeError } from '@shared/lib/describeError';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';

import { LibraryHeader } from './LibraryHeader';

const SKELETON_TILES = [0, 1, 2, 3];

function SkeletonGrid(): ReactElement {
  return (
    <View testID="library-loading" style={styles.skeletonGrid}>
      {SKELETON_TILES.map((i) => (
        <Skeleton key={i} width="47%" height={140} radius={8} />
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
      <Button testID="library-retry" label="Retry" onPress={props.onRetry} />
    </CenterMessage>
  );
}

const EMPTY_BODY = 'Tracks you add will show up here.';

export function LibraryEmptyScreen({ onDiscover }: { onDiscover: () => void }): ReactElement {
  return (
    <CenterMessage testID="library-empty" title="Your library is empty" body={EMPTY_BODY}>
      <Button label="Discover Music" onPress={onDiscover} />
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
