import { useState, type ComponentProps, type ReactElement } from 'react';
import { StyleSheet } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { EllipsisVertical } from 'lucide-react-native';

import { spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';

import { goBackOrToLibrary } from '../goBackOrToLibrary';
import { usePlaylistDelete } from '../hooks/usePlaylistDelete';
import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';
import { BackHeader } from './BackHeader';
import type { DetailProps } from './PlaylistDetailContent';

type MenuItems = ComponentProps<typeof ContextMenu>['items'];

function PlaylistGradient(): ReactElement {
  const { color } = useTheme();
  return (
    <LinearGradient
      colors={[`${color.accent}30`, `${color.accent}08`, 'transparent']}
      style={styles.gradient}
      pointerEvents="none"
    />
  );
}

function OptionsButton(props: { onPress: () => void }): ReactElement {
  return (
    <IconButton
      icon={EllipsisVertical}
      size={20}
      onPress={props.onPress}
      accessibilityLabel="Playlist options"
    />
  );
}

function PlaylistBackHeader(props: {
  router: DetailProps['router'];
  onOptions: () => void;
}): ReactElement {
  const onBack = (): void => goBackOrToLibrary(props.router);
  return (
    <BackHeader onBack={onBack}>
      <OptionsButton onPress={props.onOptions} />
    </BackHeader>
  );
}

function editItems(props: DetailProps): MenuItems {
  return [
    { label: 'Add Tracks', onPress: props.onAddTracks },
    { label: 'Rename Playlist', onPress: props.rename.startEditing },
  ];
}

function menuItems(
  props: DetailProps,
  onDelete: () => void,
  offlineAction: MenuItems[number],
): MenuItems {
  const danger: MenuItems[number] = { label: 'Delete Playlist', onPress: onDelete, tone: 'danger' };
  return [...editItems(props), offlineAction, danger];
}

function useMenuItems(props: DetailProps): MenuItems {
  const onDelete = usePlaylistDelete(props.playlistId, props.router);
  const offlineAction = usePlaylistOfflineAction(props.playlist.tracks);
  return menuItems(props, onDelete, offlineAction);
}

function useAnchorTop(): number {
  const insets = useSafeAreaInsets();
  return insets.top + spacing.xs + 44 + spacing.xs;
}

type MenuProps = DetailProps & { visible: boolean; onClose: () => void };

function PlaylistMenu(props: MenuProps): ReactElement {
  const anchorTop = useAnchorTop();
  const items = useMenuItems(props);
  const { visible, onClose } = props;
  return <ContextMenu visible={visible} onClose={onClose} anchorTop={anchorTop} items={items} />;
}

export function PlaylistTopBar(props: DetailProps): ReactElement {
  const [visible, setVisible] = useState(false);
  return (
    <>
      <PlaylistGradient />
      <PlaylistBackHeader router={props.router} onOptions={() => setVisible(true)} />
      <PlaylistMenu {...props} visible={visible} onClose={() => setVisible(false)} />
    </>
  );
}

const styles = StyleSheet.create({
  gradient: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: 350,
  },
});
