import type { ReactElement } from 'react';
import { View } from 'react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useDiscographyFilter } from '../hooks/useDiscographyFilter';
import { AlbumRail } from './AlbumRail';
import { RecordTypeChips } from './RecordTypeChips';

export function DiscographySections({
  albums,
  onAlbumPress,
}: {
  albums: DiscoveryResult[];
  onAlbumPress: (album: DiscoveryResult) => void;
}): ReactElement | null {
  const filter = useDiscographyFilter(albums);

  if (filter === null) {
    return null;
  }

  const { present, active, capped, hasMore, select, expand } = filter;

  return (
    <View>
      <RecordTypeChips present={present} active={active} onSelect={select} />
      <AlbumRail
        items={capped}
        hasMore={hasMore}
        typeKey={active.type}
        typeLabel={active.label}
        onAlbumPress={onAlbumPress}
        onSeeAll={expand}
      />
    </View>
  );
}
