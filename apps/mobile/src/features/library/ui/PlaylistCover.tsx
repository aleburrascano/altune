import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';
import { Image } from 'expo-image';

import { useTheme } from '@shared/ui';

type PlaylistCoverProps = {
  artworkUrls: string[];
  size: number;
};

export function PlaylistCover({ artworkUrls, size }: PlaylistCoverProps): ReactElement {
  const theme = useTheme();
  const half = size / 2;
  const cells = [artworkUrls[0], artworkUrls[1], artworkUrls[2], artworkUrls[3]];

  return (
    <View style={[styles.container, { width: size, height: size, borderRadius: 8, overflow: 'hidden' }]}>
      {cells.map((url, i) => (
        <View
          key={i}
          style={{
            width: half,
            height: half,
            backgroundColor: theme.color.surface2,
          }}
        >
          {url != null ? (
            <Image source={{ uri: url }} style={{ width: half, height: half }} contentFit="cover" />
          ) : null}
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
});
