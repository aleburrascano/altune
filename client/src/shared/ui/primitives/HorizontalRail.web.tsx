import { useEffect, useRef, type ReactElement, type RefObject } from 'react';
import { FlatList } from 'react-native';

import type { HorizontalRailProps } from '@shared/ui/primitives/HorizontalRail';

type WheelListener = (event: WheelEvent) => void;

interface WheelTarget {
  scrollLeft: number;
  scrollWidth: number;
  clientWidth: number;
  addEventListener: (
    type: 'wheel',
    listener: WheelListener,
    options: AddEventListenerOptions,
  ) => void;
  removeEventListener: (type: 'wheel', listener: WheelListener) => void;
}

const NON_PASSIVE: AddEventListenerOptions = { passive: false };

function isWheelTarget(value: unknown): value is WheelTarget {
  return typeof value === 'object' && value !== null && 'scrollWidth' in value;
}

function canScrollFurther(node: WheelTarget, deltaY: number): boolean {
  const maxScrollLeft = node.scrollWidth - node.clientWidth;
  return deltaY > 0 ? node.scrollLeft < maxScrollLeft : node.scrollLeft > 0;
}

function isVerticalWheel(event: WheelEvent): boolean {
  return Math.abs(event.deltaY) > Math.abs(event.deltaX);
}

function shouldRedirect(node: WheelTarget, event: WheelEvent): boolean {
  return isVerticalWheel(event) && canScrollFurther(node, event.deltaY);
}

function railWheelHandler(node: WheelTarget): WheelListener {
  return (event) => {
    if (!shouldRedirect(node, event)) {
      return;
    }
    event.preventDefault();
    node.scrollLeft += event.deltaY;
  };
}

function scrollNodeOf<T>(listRef: RefObject<FlatList<T> | null>): WheelTarget | null {
  const node = listRef.current?.getScrollableNode();
  return isWheelTarget(node) ? node : null;
}

function attachWheelHandler(node: WheelTarget): () => void {
  const onWheel = railWheelHandler(node);
  node.addEventListener('wheel', onWheel, NON_PASSIVE);
  return () => node.removeEventListener('wheel', onWheel);
}

function useWheelToHorizontalScroll<T>(): RefObject<FlatList<T> | null> {
  const listRef = useRef<FlatList<T>>(null);
  useEffect(() => {
    const node = scrollNodeOf(listRef);
    return node ? attachWheelHandler(node) : undefined;
  }, []);
  return listRef;
}

export function HorizontalRail<T>(props: HorizontalRailProps<T>): ReactElement {
  const listRef = useWheelToHorizontalScroll<T>();
  return <FlatList {...props} ref={listRef} horizontal showsHorizontalScrollIndicator={false} />;
}
