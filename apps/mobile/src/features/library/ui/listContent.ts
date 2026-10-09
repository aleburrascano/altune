import { StyleSheet } from 'react-native';

import { spacing } from '@shared/ui';

export const listContent = StyleSheet.create({
  padded: { paddingBottom: spacing['3xl'] },
  empty: { flexGrow: 1 },
});
