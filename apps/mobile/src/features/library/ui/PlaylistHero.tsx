import type { ReactElement } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';
import { fontFamily } from '@shared/ui/theme/tokens';

import { PlaylistCover } from './PlaylistCover';

interface PlaylistData {
  name: string;
  track_count: number;
  preview_artwork_urls: string[];
}

interface PlaylistHeroProps {
  playlist: PlaylistData;
  isEditing: boolean;
  editName: string;
  onEditNameChange: (text: string) => void;
  onStartEditing: () => void;
  onConfirmRename: () => void;
}

export function PlaylistHero({
  playlist,
  isEditing,
  editName,
  onEditNameChange,
  onStartEditing,
  onConfirmRename,
}: PlaylistHeroProps): ReactElement {
  const theme = useTheme();

  return (
    <View style={styles.hero}>
      <PlaylistCover artworkUrls={playlist.preview_artwork_urls} size={160} />
      {isEditing ? (
        <TextInput
          testID="playlist-rename-input"
          value={editName}
          onChangeText={onEditNameChange}
          onSubmitEditing={onConfirmRename}
          onBlur={onConfirmRename}
          autoFocus
          maxLength={100}
          style={[
            styles.renameInput,
            { color: theme.color.textPrimary, borderBottomColor: theme.color.accent },
          ]}
        />
      ) : (
        <Pressable
          onPress={onStartEditing}
          hitSlop={8}
          accessibilityRole="button"
          accessibilityLabel={`Playlist name: ${playlist.name}`}
          accessibilityHint="Tap to rename"
        >
          <Text variant="title" testID="playlist-name">{playlist.name}</Text>
        </Pressable>
      )}
      <Text variant="label" tone="secondary">
        {playlist.track_count} {playlist.track_count === 1 ? 'track' : 'tracks'}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  hero: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  renameInput: {
    fontFamily: fontFamily.displaySemiBold,
    fontSize: 20,
    textAlign: 'center',
    borderBottomWidth: 2,
    paddingBottom: 4,
    minWidth: 200,
  },
});
