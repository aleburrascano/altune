import { useEffect, useRef, type ReactElement, type RefObject } from 'react';
import { FlatList } from 'react-native';

import type { HorizontalRailProps } from '@shared/ui/primitives/HorizontalRail';

type WheelTarget = {
  scrollLeft: number;
  scrollWidth: number;
  clientWidth: number;
  addEventListener: (type: 'wheel', listener: (event: WheelEvent) => void, options: object) => void;
  removeEventListener: (type: 'wheel', listener: (event: WheelEvent) => void) => void;
};

type ScrollableList = { getScrollableNode: () => unknown };

function canScrollFurther(node: WheelTarget, deltaY: number): boolean {
  const maxScrollLeft = node.scrollWidth - node.clientWidth;
  return deltaY > 0 ? node.scrollLeft < maxScrollLeft : node.scrollLeft > 0;
}

function isVerticalWheel(event: WheelEvent): boolean {
  return Math.abs(event.deltaY) > Math.abs(event.deltaX);
}

function railWheelHandler(node: WheelTarget): (event: WheelEvent) => void {
  return (event) => {
    if (!isVerticalWheel(event) || !canScrollFurther(node, event.deltaY)) {
      return;
    }
    event.preventDefault();
    node.scrollLeft += event.deltaY;
  };
}

function scrollNodeOf<T>(listRef: RefObject<FlatList<T> | null>): WheelTarget | null {
  const list = listRef.current as unknown as ScrollableList | null;
  return (list?.getScrollableNode() as WheelTarget | undefined) ?? null;
}

function useWheelToHorizontalScroll<T>(): RefObject<FlatList<T> | null> {
  const listRef = useRef<FlatList<T>>(null);
  useEffect(() => {
    const node = scrollNodeOf(listRef);
    if (!node) {
      return undefined;
    }
    const onWheel = railWheelHandler(node);
    node.addEventListener('wheel', onWheel, { passive: false });
    return () => node.removeEventListener('wheel', onWheel);
  }, []);
  return listRef;
}

export function HorizontalRail<T>(props: HorizontalRailProps<T>): ReactElement {
  const listRef = useWheelToHorizontalScroll<T>();
  return <FlatList {...props} ref={listRef} horizontal showsHorizontalScrollIndicator={false} />;
}
