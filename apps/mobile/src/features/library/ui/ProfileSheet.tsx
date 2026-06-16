import type { ReactElement } from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';

import { Button, Text, spacing, useTheme } from '@shared/ui';
import { useSignOut } from '@shared/auth/useSignOut';

type ProfileSheetProps = {
  visible: boolean;
  email: string;
  onClose: () => void;
};

export function ProfileSheet({ visible, email, onClose }: ProfileSheetProps): ReactElement {
  const theme = useTheme();
  const { state: signOutState, signOut } = useSignOut();
  const isPending = signOutState.kind === 'pending';

  const initial = email.length > 0 ? email[0]!.toUpperCase() : '?';

  return (
    <Modal
      testID="profile-sheet"
      visible={visible}
      transparent
      animationType="slide"
      onRequestClose={onClose}
    >
      <Pressable style={[styles.backdrop, { backgroundColor: theme.color.scrim }]} onPress={onClose}>
        <View />
      </Pressable>
      <View style={[styles.sheet, { backgroundColor: theme.color.surface1 }]}>
        <View style={[styles.handle, { backgroundColor: theme.color.border }]} />
        <View style={styles.profile}>
          <View style={[styles.avatar, { backgroundColor: theme.color.accent }]}>
            <Text variant="displayL" tone="onAccent">
              {initial}
            </Text>
          </View>
          <Text variant="bodyStrong" style={styles.email}>
            {email}
          </Text>
        </View>
        <View style={[styles.divider, { backgroundColor: theme.color.border }]} />
        <Button
          testID="profile-sign-out"
          label={isPending ? 'Signing out…' : 'Sign Out'}
          variant="ghost"
          loading={isPending}
          onPress={() => {
            void signOut();
          }}
          style={styles.signOutBtn}
        />
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    flex: 1,
  },
  sheet: {
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
  },
  handle: {
    width: 36,
    height: 4,
    borderRadius: 2,
    alignSelf: 'center',
    marginBottom: spacing.xl,
  },
  profile: {
    alignItems: 'center',
    gap: spacing.md,
    paddingBottom: spacing.xl,
  },
  avatar: {
    width: 64,
    height: 64,
    borderRadius: 32,
    alignItems: 'center',
    justifyContent: 'center',
  },
  email: { textAlign: 'center' },
  divider: { height: StyleSheet.hairlineWidth, marginBottom: spacing.lg },
  signOutBtn: { alignSelf: 'center' },
});
