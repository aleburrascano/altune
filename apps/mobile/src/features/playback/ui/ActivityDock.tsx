/**
 * ActivityDock — the unified bottom dock. Stacks the in-flight downloads bar
 * above the now-playing MiniPlayer; either is absent when inactive, so the dock
 * shows only what's live (downloads-only, playing-only, both, or nothing).
 * Replaces the bare <MiniPlayer /> mount in the tab shell.
 */

import type { ReactElement } from 'react';
import { useState } from 'react';
import { View } from 'react-native';

import { useActiveDownloads } from '@shared/acquisition/useActiveDownloads';

import { DownloadsBar } from './DownloadsBar';
import { DownloadsSheet } from './DownloadsSheet';
import { MiniPlayer } from './MiniPlayer';

export function ActivityDock(): ReactElement {
  const downloads = useActiveDownloads();
  const [sheetOpen, setSheetOpen] = useState(false);

  return (
    <View>
      {downloads.length > 0 ? (
        <DownloadsBar items={downloads} onPress={() => setSheetOpen(true)} />
      ) : null}
      <MiniPlayer />
      <DownloadsSheet
        visible={sheetOpen && downloads.length > 0}
        items={downloads}
        onClose={() => setSheetOpen(false)}
      />
    </View>
  );
}
