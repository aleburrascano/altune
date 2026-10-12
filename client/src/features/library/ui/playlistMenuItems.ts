import type { ComponentProps } from 'react';

import type { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { DetailProps } from '@features/library/ui/playlistDetailTypes';

export type MenuItems = ComponentProps<typeof ContextMenu>['items'];

export type MenuEditProps = Pick<DetailProps, 'onAddTracks'> & {
  rename: Pick<DetailProps['rename'], 'startEditing'>;
};

function editItems(props: MenuEditProps): MenuItems {
  return [
    { label: 'Add Tracks', onPress: props.onAddTracks },
    { label: 'Rename Playlist', onPress: props.rename.startEditing },
  ];
}

export function menuItems(
  props: MenuEditProps,
  onDelete: () => void,
  offlineAction: MenuItems[number] | null,
  offlineSupported: boolean,
): MenuItems {
  const danger: MenuItems[number] = { label: 'Delete Playlist', onPress: onDelete, tone: 'danger' };
  const offline = offlineSupported ? offlineAction : null;
  return [...editItems(props), offline, danger].filter((item) => item !== null);
}
