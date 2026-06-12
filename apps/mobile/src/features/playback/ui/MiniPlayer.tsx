import { Pressable, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { Pause, Play } from 'lucide-react-native';

import { usePlayback } from '../hooks/usePlayback';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import type { Theme } from '@shared/ui/theme';

export function MiniPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, errorMessage } = usePlayback();
  const theme = useTheme();
  const router = useRouter();

  if (!track || status === 'idle') {
    return null;
  }

  const isPlaying = status === 'playing';
  const isError = status === 'error';
  const progress = durationMs > 0 ? positionMs / durationMs : 0;

  const handlePlayPause = async () => {
    if (isPlaying) {
      await pause();
    } else {
      await resume();
    }
  };

  const handleTap = () => {
    router.push('/player');
  };

  const s = styles(theme);

  return (
    <Pressable
      onPress={handleTap}
      style={s.container}
      accessibilityRole="button"
      accessibilityLabel={`Now playing: ${track.title} by ${track.artist}`}
    >
      <View style={s.progressBar}>
        <View style={[s.progressFill, { width: `${progress * 100}%` }]} />
      </View>
      <View style={s.content}>
        <Artwork uri={track.artworkUrl} size={40} borderRadius={6} />
        <View style={s.info}>
          <Text variant="body" numberOfLines={1}>
            {track.title}
          </Text>
          <Text variant="bodySmall" numberOfLines={1}>
            {isError ? (errorMessage ?? 'Playback error') : track.artist}
          </Text>
        </View>
        <IconButton
          icon={isPlaying ? Pause : Play}
          size={24}
          onPress={handlePlayPause}
          accessibilityLabel={isPlaying ? 'Pause' : 'Play'}
        />
      </View>
    </Pressable>
  );
}

const styles = (theme: Theme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.color.surfaceElevated,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: theme.color.border,
    },
    progressBar: {
      height: 2,
      backgroundColor: theme.color.border,
    },
    progressFill: {
      height: 2,
      backgroundColor: theme.color.accent,
    },
    content: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 12,
      paddingVertical: 8,
      gap: 12,
    },
    info: {
      flex: 1,
      gap: 2,
    },
  });
