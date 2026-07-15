/**
 * DownloadsSheet — the expanded list behind the DownloadsBar. One row per
 * in-flight track with its artwork and live phase caption. Plain RN Modal
 * (matches the ActionSheet style; no gorhom, Expo Go safe).
 */

import type { ReactElement } from 'react';
import { Modal, Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { useDownloadPhase, type DownloadEntry } from '@shared/acquisition/downloadStore';
import { phaseLabel } from '@shared/acquisition/stagePhase';
import { Artwork, Text, radius, spacing, useTheme } from '@shared/ui';

interface DownloadsSheetProps {
  visible: boolean;
  items: DownloadEntry[];
  onClose: () => void;
}

function DownloadRow({ item }: { item: DownloadEntry }): ReactElement {
  const phase = useDownloadPhase(item.trackId);
  return (
    <View style={styles.row}>
      <Artwork uri={item.artworkUrl} size={44} radius={radius.sm} accessibilityLabel="Album art" />
      <View style={styles.rowBody}>
        <Text variant="bodyStrong" numberOfLines={1}>
          {item.title ?? 'Track'}
        </Text>
        <Text variant="caption" tone="secondary" numberOfLines={1}>
          {item.artist ?? ''}
        </Text>
        <Text variant="caption" tone="accent">
          {phaseLabel(phase ?? 'working')}
        </Text>
      </View>
    </View>
  );
}

export function DownloadsSheet({ visible, items, onClose }: DownloadsSheetProps): ReactElement {
  const theme = useTheme();
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <Pressable
        style={[styles.scrim, { backgroundColor: theme.color.scrim }]}
        accessibilityRole="button"
        accessibilityLabel="Close downloads"
        onPress={onClose}
      />
      <View
        style={[
          styles.sheet,
          { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
        ]}
      >
        <View style={[styles.grabber, { backgroundColor: theme.color.border }]} />
        <View style={styles.header}>
          <Text variant="title">Downloads</Text>
          <Text variant="caption" tone="tertiary">
            {items.length} in progress
          </Text>
        </View>
        <ScrollView style={styles.list}>
          {items.map((item) => (
            <DownloadRow key={item.trackId} item={item} />
          ))}
        </ScrollView>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: { flex: 1 },
  sheet: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopLeftRadius: radius.xl,
    borderTopRightRadius: radius.xl,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.md,
    paddingBottom: spacing['3xl'],
    maxHeight: '70%',
  },
  grabber: { width: 36, height: 4, borderRadius: 2, alignSelf: 'center', marginBottom: spacing.lg },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'baseline',
    marginBottom: spacing.md,
  },
  list: { flexGrow: 0 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, paddingVertical: spacing.md },
  rowBody: { flex: 1, gap: 3 },
});
