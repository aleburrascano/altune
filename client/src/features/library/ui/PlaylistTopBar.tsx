import { useState, type ReactElement } from 'react';
import { StyleSheet } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { EllipsisVertical } from 'lucide-react-native';
import { spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import { goBackOrToLibrary } from '@features/library/domain/libraryRoutes';
import { useLibraryOffline } from '@features/library/hooks/useLibraryOffline';
import {
  usePlaylistDelete,
  usePlaylistOfflineAction,
} from '@features/library/hooks/usePlaylistMenu';
import { BackHeader } from '@features/library/ui/BackHeader';
import { menuItems, type MenuItems } from '@features/library/ui/playlistMenuItems';
import type { DetailProps } from '@features/library/ui/playlistDetailTypes';

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
      action="library.playlist.options"
      icon={EllipsisVertical}
      size={20}
      onPress={props.onPress}
      accessibilityLabel="Playlist options"
    />
  );
}

type BackHeaderProps = { router: DetailProps['router']; onOptions: () => void };

function PlaylistBackHeader(props: BackHeaderProps): ReactElement {
  const onBack = (): void => goBackOrToLibrary(props.router);
  return (
    <BackHeader onBack={onBack}>
      <OptionsButton onPress={props.onOptions} />
    </BackHeader>
  );
}

function useMenuItems(props: DetailProps): MenuItems {
  const onDelete = usePlaylistDelete(props.playlistId, props.router);
  const { supported } = useLibraryOffline();
  const offlineAction = usePlaylistOfflineAction(props.playlist.tracks);
  return menuItems(props, onDelete, offlineAction, supported);
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

const HERO_BACKDROP_HEIGHT = 350;

const styles = StyleSheet.create({
  gradient: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: HERO_BACKDROP_HEIGHT,
  },
});
