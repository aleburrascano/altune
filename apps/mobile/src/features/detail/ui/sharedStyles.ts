import { StyleSheet } from 'react-native';

import { spacing } from '@shared/ui/theme/tokens';

export const sharedStyles = StyleSheet.create({
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 48,
  },
  trackInfo: { flex: 1 },
  retryButton: { marginTop: spacing.sm },
  sectionTitle: { marginBottom: spacing.sm },
  pressed: { opacity: 0.6 },
});
