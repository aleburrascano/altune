import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { Image } from 'expo-image';
import { Music } from 'lucide-react-native';

import { useTheme } from '@shared/ui';

type PlaylistCoverProps = {
  artworkUrls: string[];
  size: number;
};

export function PlaylistCover({ artworkUrls, size }: PlaylistCoverProps): ReactElement {
  const theme = useTheme();
  const count = artworkUrls.filter(Boolean).length;

  if (count === 0) {
    return (
      <View style={[styles.container, { width: size, height: size, borderRadius: 8, backgroundColor: theme.color.surface2 }]}>
        <Music size={size * 0.3} color={theme.color.textTertiary} strokeWidth={1.5} style={{ opacity: 0.3 }} />
      </View>
    );
  }

  if (count === 1) {
    return (
      <View style={{ width: size, height: size, borderRadius: 8, overflow: 'hidden' }}>
        <Image source={artworkUrls[0]!} style={{ width: size, height: size }} contentFit="cover" />
      </View>
    );
  }

  if (count === 2) {
    const half = size / 2;
    return (
      <View style={[styles.splitContainer, { width: size, height: size, borderRadius: 8, overflow: 'hidden' }]}>
        <Image source={artworkUrls[0]!} style={{ width: half, height: size }} contentFit="cover" />
        <Image source={artworkUrls[1]!} style={{ width: half, height: size }} contentFit="cover" />
      </View>
    );
  }

  const half = size / 2;
  const cells = [artworkUrls[0], artworkUrls[1], artworkUrls[2], artworkUrls[3]];

  return (
    <View style={[styles.gridContainer, { width: size, height: size, borderRadius: 8, overflow: 'hidden' }]}>
      {cells.map((url, i) => (
        <View
          key={i}
          style={{ width: half, height: half, backgroundColor: theme.color.surface2 }}
        >
          {url != null ? (
            <Image source={{ uri: url }} style={{ width: half, height: half }} contentFit="cover" />
          ) : (
            <View style={[styles.iconCell, { width: half, height: half }]}>
              <Music size={half * 0.3} color={theme.color.textTertiary} strokeWidth={1.5} style={{ opacity: 0.3 }} />
            </View>
          )}
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  splitContainer: {
    flexDirection: 'row',
  },
  gridContainer: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  iconCell: {
    alignItems: 'center',
    justifyContent: 'center',
  },
});
