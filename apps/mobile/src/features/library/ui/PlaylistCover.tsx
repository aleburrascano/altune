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
  const urls = artworkUrls.filter(Boolean);

  if (urls.length === 0) {
    return (
      <View style={[styles.container, { width: size, height: size, borderRadius: 8, backgroundColor: theme.color.surface2 }]}>
        <Music size={size * 0.3} color={theme.color.textTertiary} strokeWidth={1.5} style={{ opacity: 0.3 }} />
      </View>
    );
  }

  if (urls.length === 1) {
    return (
      <View style={{ width: size, height: size, borderRadius: 8, overflow: 'hidden' }}>
        <Image source={{ uri: urls[0]! }} style={{ width: size, height: size }} contentFit="cover" />
      </View>
    );
  }

  if (urls.length === 2) {
    const half = size / 2;
    return (
      <View style={[styles.splitContainer, { width: size, height: size, borderRadius: 8, overflow: 'hidden' }]}>
        <Image source={{ uri: urls[0]! }} style={{ width: half, height: size }} contentFit="cover" />
        <Image source={{ uri: urls[1]! }} style={{ width: half, height: size }} contentFit="cover" />
      </View>
    );
  }

  const half = size / 2;
  const cells = [urls[0], urls[1], urls[2], urls[3]];

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
