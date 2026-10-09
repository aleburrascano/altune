import type { LucideIcon } from 'lucide-react-native';
import type { ReactElement, ReactNode } from 'react';
import { KeyboardAvoidingView, Modal, Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Button, IconBadge, Text, radius, spacing, useTheme } from '@shared/ui';
import { keyboardAvoidingBehavior } from '@shared/ui/keyboard/keyboard';

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
    <SettingsModal visible={visible} onClose={onClose} testID={testID}>
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
        <Button
          action="settings.confirm.cancel"
          label="Cancel"
          variant="secondary"
          onPress={onClose}
          style={styles.action}
        />
        <Button
          action="settings.confirm.accept"
          testID={`${testID}-confirm`}
          label={confirmLabel}
          onPress={confirm}
          style={[styles.action, { backgroundColor: theme.color.danger }]}
        />
      </View>
    </SettingsModal>
  );
}

const styles = StyleSheet.create({
  header: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, marginBottom: spacing.md },
  title: { flex: 1 },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  action: { flex: 1 },
});

type SettingsModalProps = {
  visible: boolean;
  onClose: () => void;
  testID?: string | undefined;
  children: ReactNode;
};

export function SettingsModal({
  visible,
  onClose,
  testID,
  children,
}: SettingsModalProps): ReactElement {
  const theme = useTheme();
  return (
    <Modal
      testID={testID}
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <Pressable
        style={[modalStyles.backdrop, { backgroundColor: theme.color.scrim }]}
        onPress={onClose}
        accessibilityRole="button"
        accessibilityLabel="Close"
      />
      <KeyboardAvoidingView
        style={modalStyles.centering}
        behavior={keyboardAvoidingBehavior}
        pointerEvents="box-none"
      >
        <ScrollView
          style={modalStyles.scroll}
          contentContainerStyle={modalStyles.scrollContent}
          keyboardShouldPersistTaps="handled"
        >
          <Pressable
            style={[
              modalStyles.card,
              { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
            ]}
          >
            {children}
          </Pressable>
        </ScrollView>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const modalStyles = StyleSheet.create({
  backdrop: StyleSheet.absoluteFill,
  centering: { flex: 1, justifyContent: 'center' },
  scroll: { flexGrow: 0 },
  scrollContent: { padding: spacing.xl },
  card: { borderRadius: radius.xl, borderWidth: StyleSheet.hairlineWidth, padding: spacing.xl },
});
