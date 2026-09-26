import { type ReactElement } from 'react';

import { Play } from 'lucide-react-native';

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

function scaffoldContentProps(album: AlbumDetailState, discoveryResult: DiscoveryResult, mbYear?: number) {
  return {
    facts: <DetailFacts facts={buildAlbumFacts(album.tracks, albumYearFor(discoveryResult, mbYear))} testID="detail-album-meta" />,
    actions: <DetailActions primary={albumPrimaryAction(album)} secondary={albumSecondary(album)} />,
  };
}

export function AlbumDetailBody(props: AlbumDetailBodyProps): ReactElement {
  const album = useAlbumDetailState(props.result, props.detailRoute);
  return (
    <DetailScaffold {...props.chrome} {...scaffoldContentProps(album, props.result, props.mbYear)}>
      <AlbumTrackList album={album} />
    </DetailScaffold>
  );
}
