import { StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';

import { usePlayback } from '../hooks/usePlayback';
import { Scrubber } from './Scrubber';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { useTheme } from '@shared/ui/theme';
import type { Theme } from '@shared/ui/theme';

export function FullPlayer() {
  const { status, track, positionMs, durationMs, pause, resume, seekTo } = usePlayback();
  const theme = useTheme();
  const router = useRouter();

  if (!track) {
    return null;
  }

  const isPlaying = status === 'playing';

  const handlePlayPause = async () => {
    if (isPlaying) {
      await pause();
    } else {
      await resume();
    }
  };

  const handleSeek = async (ms: number) => {
    await seekTo(ms);
  };

  const handleClose = () => {
    router.back();
  };

  const s = styles(theme);

  return (
    <View style={s.container}>
      <View style={s.header}>
        <IconButton
          name="ChevronDown"
          size={28}
          onPress={handleClose}
          accessibilityLabel="Close player"
        />
      </View>
      <View style={s.artwork}>
        <Artwork uri={track.artworkUrl} size={300} borderRadius={16} />
      </View>
      <View style={s.info}>
        <Text variant="displayS" numberOfLines={1}>
          {track.title}
        </Text>
        <Text variant="body" numberOfLines={1}>
          {track.artist}
        </Text>
      </View>
      <Scrubber positionMs={positionMs} durationMs={durationMs} onSeek={handleSeek} />
      <View style={s.controls}>
        <IconButton
          name={isPlaying ? 'Pause' : 'Play'}
          size={48}
          onPress={handlePlayPause}
          accessibilityLabel={isPlaying ? 'Pause' : 'Play'}
        />
      </View>
    </View>
  );
}

const styles = (theme: Theme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.color.canvas,
      paddingTop: 16,
    },
    header: {
      flexDirection: 'row',
      justifyContent: 'flex-start',
      paddingHorizontal: 16,
      paddingBottom: 24,
    },
    artwork: {
      alignItems: 'center',
      paddingHorizontal: 24,
      paddingBottom: 32,
    },
    info: {
      paddingHorizontal: 24,
      paddingBottom: 24,
      gap: 4,
    },
    controls: {
      flexDirection: 'row',
      justifyContent: 'center',
      alignItems: 'center',
      paddingTop: 16,
      gap: 32,
    },
  });
