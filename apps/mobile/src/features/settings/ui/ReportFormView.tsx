import { Bug, HelpCircle, Lightbulb } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Banner, Button, Chip, IconBadge, Text, radius, spacing, useTheme } from '@shared/ui';
import { TextField } from '@shared/ui/primitives/TextField';
import type { ReportKind } from '@shared/api-client/feedback';
import type { ReportDiagnostics } from '../reportDiagnostics';
import { MAX_MESSAGE_LENGTH } from '../reportRules';

const KINDS: { kind: ReportKind; label: string; icon: typeof Bug }[] = [
  { kind: 'bug', label: 'Bug', icon: Bug },
  { kind: 'idea', label: 'Idea', icon: Lightbulb },
  { kind: 'confusing', label: 'Confusing', icon: HelpCircle },
];

type ReportFormViewProps = {
  kind: ReportKind | null;
  onKindChange: (kind: ReportKind) => void;
  message: string;
  onMessageChange: (message: string) => void;
  diagnostics: ReportDiagnostics;
  /** Why the last submit failed, or null when it has not failed. */
  failure: string | null;
  ready: boolean;
  sending: boolean;
  onCancel: () => void;
  onSend: () => void;
};

export function ReportFormView({
  kind,
  onKindChange,
  message,
  onMessageChange,
  diagnostics,
  failure,
  ready,
  sending,
  onCancel,
  onSend,
}: ReportFormViewProps): ReactElement {
  const theme = useTheme();
  return (
    <>
      <View style={styles.header}>
        <IconBadge size={34} radius={radius.sm} background={theme.color.accentTint}>
          <Lightbulb size={18} color={theme.color.accentText} />
        </IconBadge>
        <Text variant="title" style={styles.headerTitle}>
          Report an issue
        </Text>
      </View>
      <Text variant="caption" tone="secondary">
        Goes straight to the developer, sent from your signed-in account.
      </Text>

      {failure !== null ? (
        <Banner tone="danger" testID="report-issue-error" style={styles.banner}>
          {failure}
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
            onPress={() => onKindChange(value)}
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
        onChangeText={onMessageChange}
        placeholder="Tell me what you expected and what you got…"
        multiline
        rows={4}
        maxLength={MAX_MESSAGE_LENGTH}
        surface="surface2"
      />

      <Text variant="caption" tone="tertiary" style={styles.diagnostics}>
        Your message is filed as an issue in Altune's public GitHub tracker, so anyone can read it.
        Don't include emails, tokens or other private details.
      </Text>

      <Text variant="caption" tone="tertiary" style={styles.diagnostics}>
        Sends with it:{' '}
        {`Altune ${diagnostics.app_version} · ${diagnostics.platform} ${diagnostics.os_version} · ${diagnostics.screen}`}
      </Text>

      <View style={styles.actions}>
        <Button label="Cancel" variant="secondary" onPress={onCancel} style={styles.action} />
        <Button
          testID="report-issue-send"
          label={failure !== null ? 'Try again' : 'Send'}
          onPress={onSend}
          disabled={!ready}
          loading={sending}
          style={styles.action}
        />
      </View>
    </>
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
});
