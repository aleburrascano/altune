import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { useWideWebLayout } from '@shared/ui/layout';
import { spacing } from '@shared/ui/theme/tokens';

import { DETAIL_GUTTER, WIDE_LEFT_COLUMN_WIDTH } from './layout';

export type DetailBodyLayoutProps = {
  hero?: ReactNode;
  actions: ReactNode;
  facts?: ReactNode;
  children: ReactNode;
};

type CompactBodyProps = Omit<DetailBodyLayoutProps, 'hero'>;

function CompactBody(props: CompactBodyProps): ReactElement {
  return (
    <View testID="detail-body" style={styles.body}>
      {props.actions}
      {props.facts}
      {props.children}
    </View>
  );
}

function WideLeftColumn({ hero, actions }: Pick<DetailBodyLayoutProps, 'hero' | 'actions'>): ReactElement {
  return (
    <View testID="detail-body-left" style={styles.left}>
      {hero}
      {actions}
    </View>
  );
}

type WideRightColumnProps = { facts: ReactNode; children: ReactNode };

function WideRightColumn({ facts, children }: WideRightColumnProps): ReactElement {
  return (
    <View testID="detail-body-right" style={styles.right}>
      {facts}
      {children}
    </View>
  );
}

function WideBody(props: DetailBodyLayoutProps): ReactElement {
  return (
    <View testID="detail-body-wide" style={styles.wideBody}>
      <WideLeftColumn hero={props.hero} actions={props.actions} />
      <WideRightColumn facts={props.facts}>{props.children}</WideRightColumn>
    </View>
  );
}

export function DetailBodyLayout(props: DetailBodyLayoutProps): ReactElement {
  const wide = useWideWebLayout();
  if (wide && props.hero != null) return <WideBody {...props} />;
  return (
    <CompactBody actions={props.actions} facts={props.facts}>
      {props.children}
    </CompactBody>
  );
}

const styles = StyleSheet.create({
  body: { paddingHorizontal: DETAIL_GUTTER },
  wideBody: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: spacing.xl,
    paddingHorizontal: DETAIL_GUTTER,
  },
  left: { width: WIDE_LEFT_COLUMN_WIDTH, flexShrink: 0 },
  right: { flex: 1, minWidth: 0 },
});
