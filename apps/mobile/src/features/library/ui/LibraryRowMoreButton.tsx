import { useRef, type ReactElement } from 'react';
import { Pressable, StyleSheet, useWindowDimensions, type View } from 'react-native';

import { MoreVertical } from 'lucide-react-native';

import type { TrackId } from '@shared/api-client/ids';
import { useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

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
