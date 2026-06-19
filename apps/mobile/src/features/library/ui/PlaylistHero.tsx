import type { ReactElement } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { LinearGradient } from 'expo-linear-gradient';
import { Play, Shuffle } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';
import { fontFamily } from '@shared/ui/theme/tokens';

import { PlaylistCover } from './PlaylistCover';

interface PlaylistData {
  name: string;
  track_count: number;
  preview_artwork_urls: string[];
  tracks?: Array<{ duration_seconds: number | null }>;
}

interface PlaylistHeroProps {
  playlist: PlaylistData;
  isEditing: boolean;
  editName: string;
  onEditNameChange: (text: string) => void;
  onStartEditing: () => void;
  onConfirmRename: () => void;
  onPlay: () => void;
  onShuffle: () => void;
}

function formatTotalDuration(tracks: Array<{ duration_seconds: number | null }> | undefined): string {
  if (!tracks || tracks.length === 0) return '';
  const totalSec = tracks.reduce((sum, t) => sum + (t.duration_seconds ?? 0), 0);
  if (totalSec === 0) return '';
  const hours = Math.floor(totalSec / 3600);
  const mins = Math.ceil((totalSec % 3600) / 60);
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

export function PlaylistHero({
  playlist,
  isEditing,
  editName,
  onEditNameChange,
  onStartEditing,
  onConfirmRename,
  onPlay,
  onShuffle,
}: PlaylistHeroProps): ReactElement {
  const theme = useTheme();
  const duration = formatTotalDuration(playlist.tracks);
  const meta = `${playlist.track_count} ${playlist.track_count === 1 ? 'track' : 'tracks'}${duration ? ` · ${duration}` : ''}`;

  return (
    <View style={styles.wrapper}>
      <LinearGradient
        colors={[`${theme.color.accent}30`, `${theme.color.accent}08`, 'transparent']}
        style={styles.gradient}
      />
      <View style={styles.hero}>
        <View style={styles.coverShadow}>
          <PlaylistCover artworkUrls={playlist.preview_artwork_urls} size={160} />
        </View>
        {isEditing ? (
          <TextInput
            testID="playlist-rename-input"
            value={editName}
            onChangeText={onEditNameChange}
            onSubmitEditing={onConfirmRename}
            onBlur={onConfirmRename}
            autoFocus
            maxLength={100}
            accessibilityLabel="Playlist name"
            accessibilityHint="Edit the playlist name"
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
            <Text variant="displayL" testID="playlist-name" style={styles.name}>{playlist.name}</Text>
          </Pressable>
        )}
        <Text variant="caption" tone="secondary">{meta}</Text>

        <View style={styles.buttons}>
          <Pressable
            onPress={onPlay}
            style={[styles.playBtn, { backgroundColor: theme.color.accent }]}
            accessibilityRole="button"
            accessibilityLabel="Play all"
          >
            <Play size={14} color={theme.color.onAccent} fill={theme.color.onAccent} />
            <Text variant="label" style={{ color: theme.color.onAccent }}>Play</Text>
          </Pressable>
          <Pressable
            onPress={onShuffle}
            style={[styles.shuffleBtn, { backgroundColor: theme.color.surface1, borderColor: theme.color.border }]}
            accessibilityRole="button"
            accessibilityLabel="Shuffle play"
          >
            <Shuffle size={14} color={theme.color.textPrimary} />
            <Text variant="label">Shuffle</Text>
          </Pressable>
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrapper: {
    position: 'relative',
  },
  gradient: {
    ...StyleSheet.absoluteFillObject,
    height: 300,
  },
  hero: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingTop: spacing.md,
    paddingBottom: spacing.xl,
  },
  coverShadow: {
    elevation: 16,
  },
  name: {
    textAlign: 'center',
    paddingHorizontal: spacing.xl,
  },
  renameInput: {
    fontFamily: fontFamily.displaySemiBold,
    fontSize: 20,
    textAlign: 'center',
    borderBottomWidth: 2,
    paddingBottom: 4,
    minWidth: 200,
  },
  buttons: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.sm,
    width: '100%',
    maxWidth: 240,
  },
  playBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.sm + 2,
    borderRadius: 999,
  },
  shuffleBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.sm + 2,
    borderRadius: 999,
    borderWidth: 1,
  },
});
