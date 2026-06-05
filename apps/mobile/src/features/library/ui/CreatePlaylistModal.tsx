import { useState, type ReactElement } from 'react';
import { Modal, Pressable, StyleSheet, TextInput, View } from 'react-native';

import { Button, Text, spacing, useTheme } from '@shared/ui';

type CreatePlaylistModalProps = {
  visible: boolean;
  onClose: () => void;
  onCreate: (name: string) => void;
  loading?: boolean;
};

export function CreatePlaylistModal({
  visible,
  onClose,
  onCreate,
  loading = false,
}: CreatePlaylistModalProps): ReactElement {
  const theme = useTheme();
  const [name, setName] = useState('');

  const handleCreate = () => {
    const trimmed = name.trim();
    if (trimmed.length > 0) {
      onCreate(trimmed);
      setName('');
    }
  };

  const handleClose = () => {
    setName('');
    onClose();
  };

  return (
    <Modal
      testID="create-playlist-modal"
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={handleClose}
    >
      <Pressable style={styles.backdrop} onPress={handleClose}>
        <View />
      </Pressable>
      <View style={styles.centered}>
        <View style={[styles.card, { backgroundColor: theme.color.surface1 }]}>
          <Text variant="title" style={styles.title}>
            New Playlist
          </Text>
          <TextInput
            testID="create-playlist-input"
            value={name}
            onChangeText={setName}
            placeholder="Playlist name"
            placeholderTextColor={theme.color.textTertiary}
            maxLength={100}
            autoFocus
            style={[
              styles.input,
              {
                color: theme.color.textPrimary,
                backgroundColor: theme.color.surface2,
                borderColor: theme.color.border,
              },
            ]}
          />
          <View style={styles.actions}>
            <Button label="Cancel" variant="ghost" onPress={handleClose} />
            <Button
              testID="create-playlist-confirm"
              label="Create"
              onPress={handleCreate}
              disabled={name.trim().length === 0}
              loading={loading}
            />
          </View>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(0,0,0,0.6)',
  },
  centered: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing['2xl'],
  },
  card: {
    width: '100%',
    borderRadius: 16,
    padding: spacing.xl,
  },
  title: { marginBottom: spacing.lg },
  input: {
    borderWidth: 1,
    borderRadius: 10,
    padding: spacing.md,
    fontSize: 16,
    marginBottom: spacing.lg,
  },
  actions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: spacing.sm,
  },
});
