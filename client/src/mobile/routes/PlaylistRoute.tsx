import { Redirect, useLocalSearchParams } from 'expo-router';
import type { ReactElement } from 'react';

import { parsePlaylistId } from '@shared/catalog';
import { useNavigator } from '@shared/navigation';
import { PlaylistDetailScreen } from '@features/library';

export function PlaylistRoute(): ReactElement {
  const params = useLocalSearchParams<{ id?: string }>();
  const navigator = useNavigator();
  const playlistId = parsePlaylistId(params.id ?? '');
  if (!playlistId.ok) return <Redirect href="/library" />;
  return <PlaylistDetailScreen playlistId={playlistId.id} navigator={navigator} />;
}
