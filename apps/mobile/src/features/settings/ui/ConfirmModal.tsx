import type { LucideIcon } from 'lucide-react-native';
import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, IconBadge, Text, radius, spacing, useTheme } from '@shared/ui';
import { Modal } from './Modal';

type ConfirmModalProps = {
  visible: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  icon: LucideIcon;
  onConfirm: () => void;
  onClose: () => void;
  testID: string;
};

export function ConfirmModal({
  visible,
  title,
  body,
  confirmLabel,
  icon: Icon,
  onConfirm,
  onClose,
  testID,
}: ConfirmModalProps): ReactElement {
  const theme = useTheme();

  const confirm = (): void => {
    onClose();
    onConfirm();
  };

  return (
    <Modal visible={visible} onClose={onClose} testID={testID}>
      <View style={styles.header}>
        <IconBadge size={34} radius={radius.sm} background={theme.color.surface2}>
          <Icon size={18} color={theme.color.danger} />
        </IconBadge>
        <Text variant="title" style={styles.title}>
          {title}
        </Text>
      </View>
      <Text tone="secondary">{body}</Text>
      <View style={styles.actions}>
        <Button label="Cancel" variant="secondary" onPress={onClose} style={styles.action} />
        <Button
          testID={`${testID}-confirm`}
          label={confirmLabel}
          onPress={confirm}
          style={[styles.action, { backgroundColor: theme.color.danger }]}
        />
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, marginBottom: spacing.md },
  title: { flex: 1 },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  action: { flex: 1 },
});
