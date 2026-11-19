export type { Href } from '@shared/navigation/adapters/expoRouterNavigator';
export {
  useNavigator,
  useRouteSegments,
  useScreenFocusEffect,
} from '@shared/navigation/adapters/expoRouterNavigator';
export type { Navigator, TabRoot } from '@shared/navigation/shell/navigator';
export { tabRootFromSegments } from '@shared/navigation/shell/navigator';
export { detailHref, readDetailHandoff } from '@shared/navigation/core/detailHandoff';
export type { DetailHandoff, DetailHref } from '@shared/navigation/core/detailHandoff';
