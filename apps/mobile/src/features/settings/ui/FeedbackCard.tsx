import { Lightbulb } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, Text, radius, spacing, useTheme } from '@shared/ui';

type FeedbackCardProps = {
  onPress: () => void;
};

export function FeedbackCard({ onPress }: FeedbackCardProps): ReactElement {
  const theme = useTheme();
  return (
    <View
      style={[
        styles.card,
        { backgroundColor: theme.color.accentTint, borderColor: theme.color.accent },
      ]}
    >
      <View style={styles.header}>
        <View style={[styles.glyph, { backgroundColor: theme.color.surface1 }]}>
          <Lightbulb size={19} color={theme.color.accentText} />
        </View>
        <View style={styles.copy}>
          <Text variant="bodyStrong">Found a bug? Got an idea?</Text>
          <Text variant="caption" tone="secondary" style={styles.subtitle}>
            Send it straight to the developer — takes 20 seconds.
          </Text>
        </View>
      </View>
      <Button
        testID="settings-report-issue"
        label="Report an issue"
        onPress={onPress}
        style={styles.action}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    marginTop: spacing.xl,
    padding: spacing.lg,
    borderRadius: radius.lg,
    borderWidth: StyleSheet.hairlineWidth,
  },
  header: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.md },
  glyph: {
    width: 36,
    height: 36,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  copy: { flex: 1 },
  subtitle: { marginTop: spacing.xs },
  action: { marginTop: spacing.md },
});
