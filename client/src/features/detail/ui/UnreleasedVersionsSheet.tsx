import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { ModalFrame } from '@shared/ui/primitives/ModalFrame';
import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { sheetGrabber, sheetTopRadius, spacing } from '@shared/ui/theme/tokens';

import type { UnreleasedTrack, UnreleasedVersion } from '@shared/apiClient/enrichment';

type UnreleasedVersionsSheetProps = {
  track: UnreleasedTrack;
  visible: boolean;
  onClose: () => void;
};

const CLOSE_ACTION = 'detail.artist.unreleased.versions.close';

type TrackProps = { track: UnreleasedTrack };
type QualityProps = { quality: string | null };
type VersionRowProps = { version: UnreleasedVersion; testID: string };

function QualityText({ quality }: QualityProps): ReactElement | null {
  if (quality === null) return null;
  return (
    <Text variant="label" tone="secondary">
      {quality}
    </Text>
  );
}

function VersionRow({ version, testID }: VersionRowProps): ReactElement {
  return (
    <View style={styles.row} testID={testID}>
      <Text variant="body">{version.label ?? version.title}</Text>
      <QualityText quality={version.quality} />
    </View>
  );
}

function VersionRows({ track }: TrackProps): ReactElement {
  const rows = track.versions.map((version, index) => (
    <VersionRow
      key={version.title}
      version={version}
      testID={`detail-unreleased-version-${track.key}-${index}`}
    />
  ));
  return <>{rows}</>;
}

function SheetTitle({ track }: TrackProps): ReactElement {
  return (
    <Text variant="title" numberOfLines={2} style={styles.title}>
      {track.title}
    </Text>
  );
}

function SheetBody({ track }: TrackProps): ReactElement {
  const { color } = useTheme();
  return (
    <View style={[styles.sheet, { backgroundColor: color.surface1 }]}>
      <View style={[styles.handle, { backgroundColor: color.border }]} />
      <SheetTitle track={track} />
      <VersionRows track={track} />
    </View>
  );
}

function frameProps({ track, visible, onClose }: UnreleasedVersionsSheetProps) {
  const testID = `detail-unreleased-versions-sheet-${track.key}`;
  return { visible, onClose, testID, variant: 'sheet' as const, closeAction: CLOSE_ACTION };
}

export function UnreleasedVersionsSheet(props: UnreleasedVersionsSheetProps): ReactElement {
  return (
    <ModalFrame {...frameProps(props)}>
      <SheetBody track={props.track} />
    </ModalFrame>
  );
}

const styles = StyleSheet.create({
  sheet: {
    borderTopLeftRadius: sheetTopRadius,
    borderTopRightRadius: sheetTopRadius,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
  },
  handle: { ...sheetGrabber, alignSelf: 'center', marginBottom: spacing.lg },
  title: { marginBottom: spacing.md },
  row: { paddingVertical: spacing.md, gap: spacing.xs },
});
