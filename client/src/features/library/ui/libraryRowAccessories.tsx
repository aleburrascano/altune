import { useRef, type ReactElement } from 'react';
import { Pressable, StyleSheet, useWindowDimensions, type View } from 'react-native';
import { MoreVertical } from 'lucide-react-native';
import type { TrackId } from '@shared/api-client/ids';
import { useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import type { PinnedStatus } from '@shared/offline/pinnedStore';
import { PINNED_GLYPH, PINNED_GLYPH_LABEL } from '@features/library/ui/pinnedGlyphs';
import { pinnedStatusDisplay } from '@features/library/domain/pinnedStatusDisplay';

export function LibraryRowMoreButton({
  trackId,
  title,
  onMore,
}: {
  trackId: TrackId;
  title: string;
  onMore: (anchor: MenuAnchor) => void;
}): ReactElement {
  const theme = useTheme();
  const buttonRef = useRef<View>(null);
  const { width: windowWidth } = useWindowDimensions();

  const openMenuAtButton = () => {
    const node = buttonRef.current;
    if (node == null) return;
    node.measureInWindow((x, y, width, height) => {
      onMore({ top: y, bottom: y + height, right: windowWidth - (x + width) });
    });
  };

  return (
    <Pressable
      ref={buttonRef}
      testID={`library-row-more-${trackId}`}
      onPress={(e) => {
        e.stopPropagation?.();
        openMenuAtButton();
      }}
      hitSlop={8}
      accessibilityRole="button"
      accessibilityLabel={`More options for ${title}`}
      style={styles.moreBtn}
    >
      <MoreVertical size={18} color={theme.color.textTertiary} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  moreBtn: {
    minWidth: 44,
    minHeight: 44,
    alignItems: 'center',
    justifyContent: 'center',
  },
});

export function LibraryRowPinnedIcon({
  trackId,
  status,
}: {
  trackId: TrackId;
  status: PinnedStatus | undefined;
}): ReactElement | null {
  const theme = useTheme();
  const { icon } = pinnedStatusDisplay(status);
  if (icon === null) return null;
  const Glyph = PINNED_GLYPH[icon.kind];
  return (
    <Glyph
      testID={`${icon.testIdPrefix}-${trackId}`}
      accessibilityLabel={PINNED_GLYPH_LABEL[icon.kind]}
      size={14}
      color={theme.color[icon.color]}
    />
  );
}
