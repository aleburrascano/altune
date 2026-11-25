import { installDialogs } from '@shared/ui/shell/dialogs';
import { platformDialogs } from '@shared/ui/view/dialog/dialog';

installDialogs(platformDialogs);

export * from '@shared/ui/view/theme';
export * from '@shared/ui/view/primitives';
export * from '@shared/ui/view/layout';
export * from '@shared/ui/shell/failure/showFailure';
export { ScreenBoundary } from '@shared/ui/view/failure/ScreenBoundary';
export { setThemePreferenceFileStore, useThemePreference } from '@shared/ui/theme/themePreference';
export { ArtworkBackground } from '@shared/ui/view/hero/ArtworkBackground';
export { TabBar } from '@shared/ui/navigation/TabBar';
export { TAB_ROUTES, pageTitleFor } from '@shared/ui/view/navigation/tabRoutes';
export type { TabRoute } from '@shared/ui/view/navigation/tabRoutes';
export {
  registerSearchFocus,
  useKeyboardShortcuts,
} from '@shared/ui/keyboard/useKeyboardShortcuts';
export { Sidebar } from '@shared/ui/view/navigation/Sidebar';
export { darkTheme } from '@shared/ui/core/theme/theme';
export { HorizontalRail } from '@shared/ui/view/primitives/HorizontalRail';
export { Artwork } from '@shared/ui/view/primitives/Artwork';
export { Tappable } from '@shared/ui/view/primitives/Tappable';
export { usePressableInteraction } from '@shared/ui/view/primitives/usePressableInteraction';
export type { PressableInteractionState } from '@shared/ui/view/primitives/usePressableInteraction';
export { asyncView } from '@shared/ui/core/async/asyncView';
export { AsyncSection } from '@shared/ui/view/async/AsyncSection';
export { countLabel, formatDuration } from '@shared/ui/core/format/displayFormat';
export { tapFeedback } from '@shared/ui/haptics';
export type { TextTone } from '@shared/ui/view/primitives/Text';
export type { ContextMenuItem } from '@shared/ui/view/primitives/ContextMenu';
export { ContextMenu } from '@shared/ui/view/primitives/ContextMenu';
export { Banner } from '@shared/ui/view/primitives/Banner';
export type { AsyncInputs, AsyncView } from '@shared/ui/core/async/asyncView';
export { SortControl } from '@shared/ui/view/primitives/SortControl';
export { TABLET_LAYOUT_MIN_WIDTH, WIDE_LAYOUT_MIN_WIDTH } from '@shared/ui/core/layout/breakpoints';
export type { MenuAnchor } from '@shared/ui/core/primitives/menuPlacement';
export { confirmDestructive, installDialogs, showAlert } from '@shared/ui/shell/dialogs';
export { platformDialogs };
export { createNativeDialogs } from '@shared/ui/view/dialog/nativeDialogs';
export type { ConfirmOptions, Dialogs } from '@shared/ui/ports/dialogs';
export { IconButton } from '@shared/ui/view/primitives/IconButton';
export type { TappableProps } from '@shared/ui/view/primitives/Tappable';
export { ModalFrame } from '@shared/ui/view/primitives/ModalFrame';
export { SearchBar } from '@shared/ui/view/primitives/SearchBar';
export { useAnnounceChange } from '@shared/ui/view/useAnnounceChange';
export { fontFamily, shadow, zLayer } from '@shared/ui/core/theme/tokens';
export { sidebarItemStyle } from '@shared/ui/view/navigation/sidebarItemStyle';
export type { ActionSheetOption } from '@shared/ui/view/primitives/ActionSheet';
export { useReduceMotion } from '@shared/ui/view/motion/useMotion';
export { ActionSheet } from '@shared/ui/view/primitives/ActionSheet';
export { TextField } from '@shared/ui/primitives/TextField';
export { AuthHeroLayout } from '@shared/ui/view/hero/AuthHeroLayout';
export { Wordmark } from '@shared/ui/view/primitives/Wordmark';
export type { SortOption } from '@shared/ui/view/primitives/SortControl';
export {
  DismissKeyboardArea,
  KeepInputFocusArea,
  adjustsKeyboardInsets,
  dismissKeyboard,
} from '@shared/ui/view/keyboard/keyboard';
export type { AsyncSectionProps } from '@shared/ui/view/async/AsyncSection';
export { announce } from '@shared/ui/view/a11y/announce';
export { useLayoutMode } from '@shared/ui/view/layout/useLayoutMode';
export type { LayoutMode } from '@shared/ui/view/layout/useLayoutMode';
