import { useCallback, useEffect, useRef, useState } from 'react';
import {
  type AccessibilityActionEvent,
  Animated,
  type GestureResponderHandlers,
  type LayoutChangeEvent,
  PanResponder,
  type View,
} from 'react-native';

import { clamp } from '../clamp';

const A11Y_SEEK_STEP_MS = 15000;

function progressRatio(positionMs: number, durationMs: number): number {
  return clamp(positionMs / durationMs, 0, 1);
}

function ratioFromPageX(pageX: number, layout: { pageX: number; width: number }): number {
  const x = pageX - layout.pageX;
  return clamp(x / (layout.width || 1), 0, 1);
}

interface UseScrubberGestureProps {
  positionMs: number;
  durationMs: number;
  onSeek: (positionMs: number) => void;
}

export function useScrubberGesture({ positionMs, durationMs, onSeek }: UseScrubberGestureProps) {
  const trackRef = useRef<View>(null);
  const layoutRef = useRef({ pageX: 0, width: 0 });

  const durationRef = useRef(durationMs);
  const onSeekRef = useRef(onSeek);
  const positionRef = useRef(positionMs);

  useEffect(() => {
    durationRef.current = durationMs;
    onSeekRef.current = onSeek;
    positionRef.current = positionMs;
  });

  const [progress] = useState(() => new Animated.Value(0));
  const isDraggingRef = useRef(false);
  const isHoldingSeek = useRef(false);
  const seekHoldTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastLabelUpdate = useRef(0);

  const [isDragging, setIsDragging] = useState(false);
  const [labelMs, setLabelMs] = useState(positionMs);

  useEffect(() => {
    if (!isDraggingRef.current && !isHoldingSeek.current && durationMs > 0) {
      progress.setValue(progressRatio(positionMs, durationMs));
      setLabelMs(positionMs);
    }
  }, [positionMs, durationMs, progress]);

  const [panHandlers, setPanHandlers] = useState<GestureResponderHandlers>({});
  useEffect(() => {
    const responder = PanResponder.create({
      onStartShouldSetPanResponder: () => true,
      onMoveShouldSetPanResponder: () => true,
      onPanResponderTerminationRequest: () => false,
      onPanResponderGrant: (evt) => {
        if (durationRef.current <= 0) return;
        isDraggingRef.current = true;
        setIsDragging(true);
        if (seekHoldTimer.current) {
          clearTimeout(seekHoldTimer.current);
          seekHoldTimer.current = null;
        }
        isHoldingSeek.current = false;
        const ratio = ratioFromPageX(evt.nativeEvent.pageX, layoutRef.current);
        progress.setValue(ratio);
        setLabelMs(ratio * durationRef.current);
        lastLabelUpdate.current = Date.now();
      },
      onPanResponderMove: (evt) => {
        if (durationRef.current <= 0) return;
        const ratio = ratioFromPageX(evt.nativeEvent.pageX, layoutRef.current);
        progress.setValue(ratio);
        const now = Date.now();
        if (now - lastLabelUpdate.current > 80) {
          lastLabelUpdate.current = now;
          setLabelMs(ratio * durationRef.current);
        }
      },
      onPanResponderRelease: (evt) => {
        isDraggingRef.current = false;
        setIsDragging(false);
        if (durationRef.current <= 0) return;
        const ratio = ratioFromPageX(evt.nativeEvent.pageX, layoutRef.current);
        const ms = ratio * durationRef.current;
        progress.setValue(ratio);
        setLabelMs(ms);
        onSeekRef.current(ms);
        isHoldingSeek.current = true;
        seekHoldTimer.current = setTimeout(() => {
          isHoldingSeek.current = false;
          if (durationRef.current > 0) {
            progress.setValue(progressRatio(positionRef.current, durationRef.current));
            setLabelMs(positionRef.current);
          }
        }, 600);
      },
      onPanResponderTerminate: () => {
        isDraggingRef.current = false;
        setIsDragging(false);
        isHoldingSeek.current = false;
        if (seekHoldTimer.current) {
          clearTimeout(seekHoldTimer.current);
          seekHoldTimer.current = null;
        }
      },
    });
    setPanHandlers(responder.panHandlers);
    return () => {
      if (seekHoldTimer.current) {
        clearTimeout(seekHoldTimer.current);
        seekHoldTimer.current = null;
      }
    };
  }, [progress]);

  const remeasure = useCallback(() => {
    trackRef.current?.measureInWindow((x, _y, width) => {
      if (width > 0) layoutRef.current = { pageX: x, width };
    });
  }, []);

  const onLayout = useCallback(
    (_e: LayoutChangeEvent) => {
      remeasure();
    },
    [remeasure],
  );

  const onAccessibilityAction = useCallback((e: AccessibilityActionEvent) => {
    const dur = durationRef.current;
    if (dur <= 0) return;
    const delta = e.nativeEvent.actionName === 'decrement' ? -A11Y_SEEK_STEP_MS : A11Y_SEEK_STEP_MS;
    const next = clamp(positionRef.current + delta, 0, dur);
    onSeekRef.current(next);
  }, []);

  const fillWidth = progress.interpolate({
    inputRange: [0, 1],
    outputRange: ['0%', '100%'],
  });
  const thumbLeft = progress.interpolate({
    inputRange: [0, 1],
    outputRange: ['0%', '100%'],
  });

  return {
    trackRef,
    panHandlers,
    isDragging,
    labelMs,
    onLayout,
    onAccessibilityAction,
    fillWidth,
    thumbLeft,
  };
}
