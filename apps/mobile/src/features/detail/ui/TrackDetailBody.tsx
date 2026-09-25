import { type ComponentProps, type ReactElement } from 'react';
import { View } from 'react-native';
import { ListPlus, Pause, Play } from 'lucide-react-native';

import { AddToPlaylistSheet } from '@shared/playlists';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { FeaturedArtist } from '@shared/api-client/types';

import {
  useTrackDetailActions,
  type LateralNavHandle,
  type TrackDetailActions,
} from '../hooks/useTrackDetailActions';
import { type DetailRoute } from '../navigation';

import { DetailActions, SecondaryAction, type PrimaryAction } from './DetailActions';
import { DetailFacts } from './DetailFacts';
import { DetailScaffold, type DetailChrome } from './DetailScaffold';
import { RelatedTracksSection } from './RelatedTracksSection';
import { TrackInfoSection } from './TrackInfoSection';
import { TrackSavePill } from './TrackSavePill';
import { TrackStatusBanners } from './TrackStatusBanners';
import { buildTrackFacts } from './trackDetailFacts';

function wrongAlbumMenu(actions: TrackDetailActions): { label: string; onPress: () => void }[] {
  if (actions.albumName === null) return [];
  const label = actions.wrongAlbumReported ? 'Thanks — noted' : 'Wrong album?';
  return [{ label, onPress: actions.onReportWrongAlbum }];
}

function playlistLabel(track: DiscoveryResult): string {
  return `${track.title}${track.subtitle != null ? ` — ${track.subtitle}` : ''}`;
}

type TrackActionsProps = { actions: TrackDetailActions; title: string };

const ADD_TO_PLAYLIST = { testID: 'detail-add-to-playlist', icon: ListPlus } as const;

function AddToPlaylistAction({ actions, title }: TrackActionsProps): ReactElement | null {
  if (!actions.canSave) return null;
  return (
    <SecondaryAction
      {...ADD_TO_PLAYLIST}
      onPress={() => actions.setPlaylistSheetVisible(true)}
      accessibilityLabel={`Add ${title} to a playlist`}
    />
  );
}

function TrackSecondary(props: TrackActionsProps): ReactElement {
  return (
    <>
      <TrackSavePill {...props} />
      <AddToPlaylistAction {...props} />
    </>
  );
}

function primaryAction(actions: TrackDetailActions): PrimaryAction {
  return {
    label: actions.playLabel,
    icon: actions.playing ? Pause : Play,
    onPress: actions.onTogglePlay,
    disabled: actions.source === null || actions.playLoading,
    testID: actions.isPreview ? 'detail-preview' : 'detail-play',
    accessibilityLabel: actions.playLabel,
  };
}

function TrackPrimary(props: TrackActionsProps): ReactElement {
  const secondary = <TrackSecondary {...props} />;
  return <DetailActions primary={primaryAction(props.actions)} secondary={secondary} />;
}

type TrackDetailBodyProps = {
  chrome: DetailChrome;
  result: DiscoveryResult;
  lateralNav: LateralNavHandle;
  detailRoute: DetailRoute;
  deezerFeatured?: FeaturedArtist[];
  mbYear?: number;
};

type ScaffoldSlots = Pick<ComponentProps<typeof DetailScaffold>, 'facts' | 'menuItems' | 'actions'>;

type ContentProps = { props: TrackDetailBodyProps; actions: TrackDetailActions };

function TrackInfoBlock({ props, actions }: ContentProps): ReactElement {
  return (
    <>
      <TrackInfoSection track={props.result} actions={actions} lateralNav={props.lateralNav} />
      <TrackStatusBanners saveFailure={actions.saveFailure} lateralNav={props.lateralNav} />
    </>
  );
}

function TrackPlaylistSheet({ props, actions }: ContentProps): ReactElement {
  return (
    <AddToPlaylistSheet
      visible={actions.playlistSheetVisible}
      label={playlistLabel(props.result)}
      resolveTrackIds={actions.resolveTrackIds}
      onClose={() => actions.setPlaylistSheetVisible(false)}
    />
  );
}

function TrackContent({ props, actions }: ContentProps): ReactElement {
  return (
    <View testID="detail-track-info">
      <TrackInfoBlock props={props} actions={actions} />
      <RelatedTracksSection result={props.result} detailRoute={props.detailRoute} />
      <TrackPlaylistSheet props={props} actions={actions} />
    </View>
  );
}

function scaffoldSlots(actions: TrackDetailActions, title: string): ScaffoldSlots {
  return {
    facts: <DetailFacts facts={buildTrackFacts(actions)} testID="detail-track-facts" />,
    menuItems: wrongAlbumMenu(actions),
    actions: <TrackPrimary actions={actions} title={title} />,
  };
}

export function TrackDetailBody(props: TrackDetailBodyProps): ReactElement {
  const actions = useTrackDetailActions(props);
  return (
    <DetailScaffold {...props.chrome} {...scaffoldSlots(actions, props.result.title)}>
      <TrackContent props={props} actions={actions} />
    </DetailScaffold>
  );
}
