import type { ReactElement } from 'react';
import { FlatList, StyleSheet } from 'react-native';

import type { TrackResponse } from '@shared/api-client/types';
import { Screen, spacing } from '@shared/ui';

import { ExpandedHeader } from './ExpandedHeader';
import { LibraryHeader } from './LibraryHeader';
import { LibraryRow } from './LibraryRow';
import { ProfileSheet } from './ProfileSheet';
import { sortTracks, TRACK_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

type ExpandedTracksProps = {
  tracks: TrackResponse[];
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  onCollapse: () => void;
  navigateToTrack: (track: TrackResponse) => void;
  onLongPress: (track: TrackResponse) => void;
  initial: string;
  email: string;
  profileVisible: boolean;
  onProfileToggle: (visible: boolean) => void;
};

export function ExpandedTracks({
  tracks,
  sortKey,
  onSortChange,
  onCollapse,
  navigateToTrack,
  onLongPress,
  initial,
  email,
  profileVisible,
  onProfileToggle,
}: ExpandedTracksProps): ReactElement {
  const sorted = sortTracks(tracks, sortKey);
  return (
    <Screen>
      <LibraryHeader initial={initial} onAvatarPress={() => onProfileToggle(true)} />
      <ExpandedHeader
        title="Recently Added"
        onCollapse={onCollapse}
        sortKey={sortKey}
        onSortChange={onSortChange}
        sortOptions={TRACK_SORT_OPTIONS}
      />
      <FlatList
        data={sorted}
        keyExtractor={(t) => t.id}
        renderItem={({ item }) => (
          <LibraryRow track={item} onPress={() => navigateToTrack(item)} onLongPress={() => onLongPress(item)} />
        )}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.expandedList}
      />
      <ProfileSheet visible={profileVisible} email={email} onClose={() => onProfileToggle(false)} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  expandedList: { paddingBottom: spacing['3xl'] },
});
