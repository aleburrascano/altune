import { CheckCheck } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, IconBadge, Text, radius, spacing, useTheme } from '@shared/ui';

type ReportSentViewProps = {
  issueNumber: number;
  onDone: () => void;
  onSendAnother: () => void;
};

export function ReportSentView({
  issueNumber,
  onDone,
  onSendAnother,
}: ReportSentViewProps): ReactElement {
  const theme = useTheme();
  return (
    <>
      <View style={styles.sent}>
        <IconBadge
          size={52}
          radius={radius.full}
          background={theme.color.surface2}
          style={styles.sentGlyph}
        >
          <CheckCheck size={26} color={theme.color.success} />
        </IconBadge>
        <Text variant="title">Sent — thank you</Text>
        <Text tone="secondary" style={styles.sentBody}>
          Filed as #{issueNumber}. Every report gets read.
        </Text>
      </View>
      <View style={styles.actions}>
        <Button
          testID="report-issue-done"
          label="Done"
          variant="secondary"
          onPress={onDone}
          style={styles.action}
        />
        <Button
          testID="report-issue-another"
          label="Send another"
          onPress={onSendAnother}
          style={styles.action}
        />
      </View>
    </>
  );
}

const styles = StyleSheet.create({
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  action: { flex: 1 },
  sent: { alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  sentGlyph: { marginBottom: spacing.sm },
  sentBody: { textAlign: 'center' },
});
