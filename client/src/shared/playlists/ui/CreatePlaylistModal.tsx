import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button, Text, spacing } from '@shared/ui';
import { ModalFrame } from '@shared/ui/primitives/ModalFrame';
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
  const [name, setName] = useState('');

  const handleCreate = () => {
    if (loading) return;
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
    <ModalFrame
      visible={visible}
      onClose={handleClose}
      closeAction="playlists.create_playlist.close"
      testID="create-playlist-modal"
    >
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
        <Button
          action="playlists.create.cancel"
          label="Cancel"
          variant="ghost"
          onPress={handleClose}
        />
        <Button
          action="playlists.create.confirm"
          testID="create-playlist-confirm"
          label="Create"
          onPress={handleCreate}
          disabled={name.trim().length === 0}
          loading={loading}
        />
      </View>
    </ModalFrame>
  );
}

const styles = StyleSheet.create({
  title: { marginBottom: spacing.lg },
  field: { marginBottom: spacing.lg },
  actions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: spacing.sm,
  },
});
