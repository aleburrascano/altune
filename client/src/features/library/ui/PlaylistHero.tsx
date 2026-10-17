import type { ReactElement } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Play, Plus, Shuffle } from 'lucide-react-native';

import { countLabel } from '@shared/ui/format/displayFormat';
import { Text, spacing, useTheme } from '@shared/ui';
import {
  fontFamily,
  borderWidth,
  pillPaddingVertical,
  radius,
  shadow,
  touchTarget,
} from '@shared/ui/theme/tokens';

import { PlaylistCover } from '@features/library/ui/PlaylistCover';

interface PlaylistData {
  name: string;
  track_count: number;
  preview_artwork_urls: string[];
  total_duration_seconds?: number;
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
  onAddTracks: () => void;
}

function formatTotalDuration(totalSeconds: number | undefined): string {
  if (totalSeconds == null || totalSeconds === 0) return '';
  const totalMinutes = Math.ceil(totalSeconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
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
  onAddTracks,
}: PlaylistHeroProps): ReactElement {
  const theme = useTheme();
  const duration = formatTotalDuration(playlist.total_duration_seconds);
  const meta = `${playlist.track_count} ${countLabel(playlist.track_count, 'track')}${duration ? ` · ${duration}` : ''}`;

  return (
    <View style={styles.wrapper}>
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
          <Tappable
            action="playlist.rename"
            onPress={onStartEditing}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={`Playlist name: ${playlist.name}`}
            accessibilityHint="Tap to rename"
          >
            <Text variant="displayL" testID="playlist-name" style={styles.name}>
              {playlist.name}
            </Text>
          </Tappable>
        )}
        <Text variant="caption" tone="secondary">
          {meta}
        </Text>

        <View style={styles.buttons}>
          <Tappable
            action="playlist.play"
            onPress={onPlay}
            style={[styles.playBtn, { backgroundColor: theme.color.accent }]}
            accessibilityRole="button"
            accessibilityLabel="Play all"
          >
            <Play size={14} color={theme.color.onAccent} fill={theme.color.onAccent} />
            <Text variant="label" style={{ color: theme.color.onAccent }}>
              Play
            </Text>
          </Tappable>
          <Tappable
            action="playlist.shuffle"
            onPress={onShuffle}
            style={[
              styles.shuffleBtn,
              { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
            ]}
            accessibilityRole="button"
            accessibilityLabel="Shuffle play"
          >
            <Shuffle size={14} color={theme.color.textPrimary} />
            <Text variant="label">Shuffle</Text>
          </Tappable>
          <Tappable
            action="playlist.addTracks"
            testID="playlist-add-tracks"
            onPress={onAddTracks}
            style={[
              styles.addBtn,
              { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
            ]}
            accessibilityRole="button"
            accessibilityLabel="Add tracks from your library"
          >
            <Plus size={18} color={theme.color.textPrimary} />
          </Tappable>
        </View>
      </View>
    </View>
  );
}

const RENAME_FONT_SIZE = 20;
const RENAME_INPUT_MIN_WIDTH = 200;
const BUTTONS_MAX_WIDTH = 290;

const styles = StyleSheet.create({
  wrapper: {
    position: 'relative',
  },
  hero: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingTop: spacing.md,
    paddingBottom: spacing.xl,
  },
  coverShadow: {
    boxShadow: shadow.cover,
  },
  name: {
    textAlign: 'center',
    paddingHorizontal: spacing.xl,
  },
  renameInput: {
    fontFamily: fontFamily.displaySemiBold,
    fontSize: RENAME_FONT_SIZE,
    textAlign: 'center',
    borderBottomWidth: borderWidth.emphasis,
    paddingBottom: spacing.xs,
    minWidth: RENAME_INPUT_MIN_WIDTH,
  },
  buttons: {
    flexDirection: 'row',
    gap: spacing.sm,
    marginTop: spacing.sm,
    width: '100%',
    maxWidth: BUTTONS_MAX_WIDTH,
  },
  playBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
    paddingVertical: pillPaddingVertical,
    borderRadius: radius.full,
  },
  addBtn: {
    alignItems: 'center',
    justifyContent: 'center',
    width: touchTarget,
    paddingVertical: pillPaddingVertical,
    borderRadius: radius.full,
    borderWidth: 1,
  },
  shuffleBtn: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
    paddingVertical: pillPaddingVertical,
    borderRadius: radius.full,
    borderWidth: 1,
  },
});
