import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import { DETAIL_GUTTER } from './layout';

export type DetailBodyLayoutProps = {
  actions: ReactNode;
  facts?: ReactNode;
  children: ReactNode;
};

export function DetailBodyLayout(props: DetailBodyLayoutProps): ReactElement {
  return (
    <View testID="detail-body" style={styles.body}>
      {props.actions}
      {props.facts}
      {props.children}
    </View>
  );
}

const styles = StyleSheet.create({
  body: { paddingHorizontal: DETAIL_GUTTER },
});
