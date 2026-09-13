import { Bug, CheckCheck, HelpCircle, Lightbulb } from 'lucide-react-native';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner, Button, Chip, IconBadge, Text, radius, spacing, useTheme } from '@shared/ui';
import { TextField } from '@shared/ui/primitives/TextField';
import type { ReportKind } from '@shared/api-client/feedback';
import { submitFailureMessage, useSubmitReport } from '../hooks/useSubmitReport';
import { Modal } from './Modal';
import { reportDiagnostics } from './reportDiagnostics';

const MIN_MESSAGE_LENGTH = 10;
const MAX_MESSAGE_LENGTH = 2000;

const KINDS: { kind: ReportKind; label: string; icon: typeof Bug }[] = [
  { kind: 'bug', label: 'Bug', icon: Bug },
  { kind: 'idea', label: 'Idea', icon: Lightbulb },
  { kind: 'confusing', label: 'Confusing', icon: HelpCircle },
];

type ReportIssueModalProps = {
  visible: boolean;
  onClose: () => void;
  screen: string;
};

export function ReportIssueModal({
  visible,
  onClose,
  screen,
}: ReportIssueModalProps): ReactElement {
  const theme = useTheme();
  const submit = useSubmitReport();
  const [kind, setKind] = useState<ReportKind | null>(null);
  const [message, setMessage] = useState('');

  const diagnostics = reportDiagnostics(screen);
  const ready = kind !== null && message.trim().length >= MIN_MESSAGE_LENGTH;

  const clearDraft = (): void => {
    submit.reset();
    setKind(null);
    setMessage('');
  };

  const close = (): void => {
    onClose();
    if (submit.isSuccess) clearDraft();
  };

  const send = (): void => {
    if (kind === null) return;
    submit.mutate({ kind, message: message.trim(), ...diagnostics });
  };

  if (submit.isSuccess) {
    return (
      <Modal visible={visible} onClose={close} testID="report-issue-modal">
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
            Filed as #{submit.data.issue_number}. Every report gets read.
          </Text>
        </View>
        <View style={styles.actions}>
          <Button
            testID="report-issue-done"
            label="Done"
            variant="secondary"
            onPress={close}
            style={styles.action}
          />
          <Button
            testID="report-issue-another"
            label="Send another"
            onPress={clearDraft}
            style={styles.action}
          />
        </View>
      </Modal>
    );
  }

  return (
    <Modal visible={visible} onClose={close} testID="report-issue-modal">
      <View style={styles.header}>
        <IconBadge size={34} radius={radius.sm} background={theme.color.accentTint}>
          <Lightbulb size={18} color={theme.color.accentText} />
        </IconBadge>
        <Text variant="title" style={styles.headerTitle}>
          Report an issue
        </Text>
      </View>
      <Text variant="caption" tone="secondary">
        Goes straight to the developer. No account needed.
      </Text>

      {submit.isError ? (
        <Banner tone="danger" testID="report-issue-error" style={styles.banner}>
          {submitFailureMessage(submit.error)}
        </Banner>
      ) : null}

      <Text variant="overline" tone="tertiary" style={styles.fieldLabel}>
        What kind?
      </Text>
      <View style={styles.chips}>
        {KINDS.map(({ kind: value, label, icon: Icon }) => (
          <Chip
            key={value}
            testID={`report-issue-kind-${value}`}
            label={label}
            selected={kind === value}
            onPress={() => setKind(value)}
            icon={
              <Icon
                size={15}
                color={kind === value ? theme.color.onAccent : theme.color.textSecondary}
              />
            }
          />
        ))}
      </View>

      <Text variant="overline" tone="tertiary" style={styles.fieldLabel}>
        What happened?
      </Text>
      <TextField
        testID="report-issue-message"
        value={message}
        onChangeText={setMessage}
        placeholder="Tell me what you expected and what you got…"
        multiline
        rows={4}
        maxLength={MAX_MESSAGE_LENGTH}
        surface="surface2"
      />

      <Text variant="caption" tone="tertiary" style={styles.diagnostics}>
        Sends with it:{' '}
        {`Altune ${diagnostics.app_version} · ${diagnostics.platform} ${diagnostics.os_version} · ${diagnostics.screen}`}
      </Text>

      <View style={styles.actions}>
        <Button label="Cancel" variant="secondary" onPress={close} style={styles.action} />
        <Button
          testID="report-issue-send"
          label={submit.isError ? 'Try again' : 'Send'}
          onPress={send}
          disabled={!ready}
          loading={submit.isPending}
          style={styles.action}
        />
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, marginBottom: spacing.xs },
  headerTitle: { flex: 1 },
  banner: { marginTop: spacing.lg },
  fieldLabel: { marginTop: spacing.xl, marginBottom: spacing.sm },
  chips: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  diagnostics: { marginTop: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  action: { flex: 1 },
  sent: { alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  sentGlyph: { marginBottom: spacing.sm },
  sentBody: { textAlign: 'center' },
});
