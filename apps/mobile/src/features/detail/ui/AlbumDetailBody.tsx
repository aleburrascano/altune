import { type ReactElement } from 'react';

import { View } from 'react-native';

import { Play } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useAlbumDetailState, type AlbumDetailState } from '../hooks/useAlbumDetailState';
import type { DetailRoute } from '../navigation';

import { albumYear } from './formatters';
import { buildAlbumFacts } from './albumDetailFacts';
import { AlbumTrackList } from './AlbumTrackList';
import { DetailActions } from './DetailActions';
import { DetailFacts } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { SaveAllPill } from './SaveAllPill';

type AlbumDetailBodyProps = {
  chrome: DetailChrome;
  result: DiscoveryResult;
  detailRoute: DetailRoute;
  mbYear?: number;
};

function albumYearFor(discoveryResult: DiscoveryResult, mbYear?: number): string | null {
  return mbYear != null && mbYear > 0 ? String(mbYear) : albumYear(discoveryResult);
}

function albumPrimaryAction(album: AlbumDetailState) {
  return {
    label: album.playButton.label,
    icon: Play,
    onPress: album.onPlayOwned,
    disabled: album.playButton.disabled,
    testID: 'detail-album-play',
    accessibilityLabel: album.playButton.label,
  };
}

function albumSecondary(album: AlbumDetailState) {
  return (
    <SaveAllPill
      unownedCount={album.owned.unownedCount}
      saving={album.savingAll}
      onSave={album.onSaveAll}
    />
  );
}

function SaveAllFailure({ album }: { album: AlbumDetailState }): ReactElement | null {
  const failed = album.lastBatch?.failed ?? 0;
  if (failed === 0) return null;
  return (
    <Text variant="body" tone="danger" testID="detail-save-all-failed">
      {`${failed} couldn't be saved`}
    </Text>
  );
}

function albumActions(album: AlbumDetailState) {
  return (
    <View>
      <DetailActions primary={albumPrimaryAction(album)} secondary={albumSecondary(album)} />
      <SaveAllFailure album={album} />
    </View>
  );
}

function albumFacts(album: AlbumDetailState, discoveryResult: DiscoveryResult, mbYear?: number) {
  return (
    <DetailFacts
      facts={buildAlbumFacts(album.tracks, albumYearFor(discoveryResult, mbYear))}
      testID="detail-album-meta"
    />
  );
}

type ScaffoldArgs = {
  album: AlbumDetailState;
  discoveryResult: DiscoveryResult;
  mbYear?: number | undefined;
};

function scaffoldContentProps({ album, discoveryResult, mbYear }: ScaffoldArgs) {
  return {
    facts: albumFacts(album, discoveryResult, mbYear),
    actions: albumActions(album),
  };
}

export function AlbumDetailBody(props: AlbumDetailBodyProps): ReactElement {
  const album = useAlbumDetailState(props.result, props.detailRoute);
  const scaffoldProps = scaffoldContentProps({
    album,
    discoveryResult: props.result,
    mbYear: props.mbYear,
  });
  return (
    <DetailScaffold {...props.chrome} {...scaffoldProps}>
      <AlbumTrackList album={album} />
    </DetailScaffold>
  );
}
