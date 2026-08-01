import { useState, type ReactElement } from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';

import { Button, Text, radius, spacing, useTheme } from '@shared/ui';
import { TextField } from '@shared/ui/primitives/TextField';

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
      <Pressable
        style={[styles.backdrop, { backgroundColor: theme.color.scrim }]}
        onPress={handleClose}
        accessibilityRole="button"
        accessibilityLabel="Close"
      >
        <View />
      </Pressable>
      <View style={styles.centered}>
        <View style={[styles.card, { backgroundColor: theme.color.surface1 }]}>
          <Text variant="title" style={styles.title}>
            New Playlist
          </Text>
          <View style={styles.field}>
            <TextField
              testID="create-playlist-input"
              value={name}
              onChangeText={setName}
              placeholder="Playlist name"
              maxLength={100}
              autoFocus
              returnKeyType="done"
              onSubmitEditing={handleCreate}
              surface="surface2"
            />
          </View>
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
  },
  centered: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    padding: spacing['2xl'],
  },
  card: {
    width: '100%',
    borderRadius: radius.lg,
    padding: spacing.xl,
  },
  title: { marginBottom: spacing.lg },
  field: { marginBottom: spacing.lg },
  actions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: spacing.sm,
  },
});
