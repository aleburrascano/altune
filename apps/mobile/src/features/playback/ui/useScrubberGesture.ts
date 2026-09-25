import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  type AccessibilityActionEvent,
  Animated,
  type GestureResponderEvent,
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

interface ScrubCtx {
  progress: Animated.Value;
  setIsDragging: (dragging: boolean) => void;
  setLabelMs: (ms: number) => void;
  durationRef: { current: number };
  onSeekRef: { current: (positionMs: number) => void };
  positionRef: { current: number };
  layoutRef: { current: { pageX: number; width: number } };
  isDraggingRef: { current: boolean };
  isHoldingSeek: { current: boolean };
  seekHoldTimer: { current: ReturnType<typeof setTimeout> | null };
  lastLabelUpdate: { current: number };
}

type ScrubFlags = Pick<
  ScrubCtx,
  'layoutRef' | 'isDraggingRef' | 'isHoldingSeek' | 'seekHoldTimer' | 'lastLabelUpdate'
>;

const ALWAYS_RESPOND = {
  onStartShouldSetPanResponder: () => true,
  onMoveShouldSetPanResponder: () => true,
  onPanResponderTerminationRequest: () => false,
};

function useLatest<T>(value: T) {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
}

function useGestureFlags(): ScrubFlags {
  const [flags] = useState<ScrubFlags>(() => ({
    layoutRef: { current: { pageX: 0, width: 0 } },
    isDraggingRef: { current: false },
    isHoldingSeek: { current: false },
    seekHoldTimer: { current: null },
    lastLabelUpdate: { current: 0 },
  }));
  return flags;
}

function useScrubDisplay(positionMs: number) {
  const [progress] = useState(() => new Animated.Value(0));
  const [isDragging, setIsDragging] = useState(false);
  const [labelMs, setLabelMs] = useState(positionMs);
  return { progress, isDragging, setIsDragging, labelMs, setLabelMs };
}

function useLatestRefs({ positionMs, durationMs, onSeek }: UseScrubberGestureProps) {
  const durationRef = useLatest(durationMs);
  const onSeekRef = useLatest(onSeek);
  const positionRef = useLatest(positionMs);
  return useMemo(
    () => ({ durationRef, onSeekRef, positionRef }),
    [durationRef, onSeekRef, positionRef],
  );
}

type ScrubSetters = Pick<ScrubCtx, 'progress' | 'setIsDragging' | 'setLabelMs'>;
type ScrubLatest = Pick<ScrubCtx, 'durationRef' | 'onSeekRef' | 'positionRef'>;

function useMergedCtx(flags: ScrubFlags, latest: ScrubLatest, sinks: ScrubSetters): ScrubCtx {
  const { progress, setIsDragging, setLabelMs } = sinks;
  return useMemo(
    () => ({ ...flags, ...latest, progress, setIsDragging, setLabelMs }),
    [flags, latest, progress, setIsDragging, setLabelMs],
  );
}

function useScrubCtx(props: UseScrubberGestureProps) {
  const display = useScrubDisplay(props.positionMs);
  const latest = useLatestRefs(props);
  const flags = useGestureFlags();
  const { progress, setIsDragging, setLabelMs } = display;
  const ctx = useMergedCtx(flags, latest, { progress, setIsDragging, setLabelMs });
  return { ctx, isDragging: display.isDragging, labelMs: display.labelMs };
}

function clearHoldTimer(c: ScrubCtx) {
  if (!c.seekHoldTimer.current) return;
  clearTimeout(c.seekHoldTimer.current);
  c.seekHoldTimer.current = null;
}

function showRatio(c: ScrubCtx, ratio: number) {
  c.progress.setValue(ratio);
  c.setLabelMs(ratio * c.durationRef.current);
}

function syncToPosition(c: ScrubCtx) {
  if (c.durationRef.current <= 0) return;
  c.progress.setValue(progressRatio(c.positionRef.current, c.durationRef.current));
  c.setLabelMs(c.positionRef.current);
}

function ratioOf(c: ScrubCtx, evt: GestureResponderEvent): number {
  return ratioFromPageX(evt.nativeEvent.pageX, c.layoutRef.current);
}

function onGrant(c: ScrubCtx, evt: GestureResponderEvent) {
  if (c.durationRef.current <= 0) return;
  c.isDraggingRef.current = true;
  c.setIsDragging(true);
  clearHoldTimer(c);
  c.isHoldingSeek.current = false;
  showRatio(c, ratioOf(c, evt));
  c.lastLabelUpdate.current = Date.now();
}

function onMove(c: ScrubCtx, evt: GestureResponderEvent) {
  if (c.durationRef.current <= 0) return;
  const ratio = ratioOf(c, evt);
  c.progress.setValue(ratio);
  const now = Date.now();
  if (now - c.lastLabelUpdate.current <= 80) return;
  c.lastLabelUpdate.current = now;
  c.setLabelMs(ratio * c.durationRef.current);
}

function holdSeek(c: ScrubCtx) {
  c.isHoldingSeek.current = true;
  c.seekHoldTimer.current = setTimeout(() => {
    c.isHoldingSeek.current = false;
    syncToPosition(c);
  }, 600);
}

function onRelease(c: ScrubCtx, evt: GestureResponderEvent) {
  c.isDraggingRef.current = false;
  c.setIsDragging(false);
  if (c.durationRef.current <= 0) return;
  const ratio = ratioOf(c, evt);
  showRatio(c, ratio);
  c.onSeekRef.current(ratio * c.durationRef.current);
  holdSeek(c);
}

function onTerminate(c: ScrubCtx) {
  c.isDraggingRef.current = false;
  c.setIsDragging(false);
  c.isHoldingSeek.current = false;
  clearHoldTimer(c);
}

function createResponder(c: ScrubCtx) {
  return PanResponder.create({
    ...ALWAYS_RESPOND,
    onPanResponderGrant: (evt) => onGrant(c, evt),
    onPanResponderMove: (evt) => onMove(c, evt),
    onPanResponderRelease: (evt) => onRelease(c, evt),
    onPanResponderTerminate: () => onTerminate(c),
  });
}

function useFollowPosition(c: ScrubCtx, positionMs: number, durationMs: number) {
  useEffect(() => {
    if (c.isDraggingRef.current || c.isHoldingSeek.current || durationMs <= 0) return;
    c.progress.setValue(progressRatio(positionMs, durationMs));
  }, [c, positionMs, durationMs]);
}

function usePanHandlers(c: ScrubCtx) {
  useEffect(() => () => clearHoldTimer(c), [c]);
  return useMemo(() => createResponder(c).panHandlers, [c]);
}

function useTrackLayout(c: ScrubCtx) {
  const trackRef = useRef<View>(null);
  const onLayout = useCallback(() => {
    trackRef.current?.measureInWindow((x, _y, width) => {
      if (width > 0) c.layoutRef.current = { pageX: x, width };
    });
  }, [c]);
  return { trackRef, onLayout };
}

function stepSeek(c: ScrubCtx, actionName: string) {
  const dur = c.durationRef.current;
  if (dur <= 0) return;
  const step = actionName === 'decrement' ? -A11Y_SEEK_STEP_MS : A11Y_SEEK_STEP_MS;
  c.onSeekRef.current(clamp(c.positionRef.current + step, 0, dur));
}

function useA11yAction(c: ScrubCtx) {
  return useCallback((e: AccessibilityActionEvent) => stepSeek(c, e.nativeEvent.actionName), [c]);
}

function useTouch(c: ScrubCtx) {
  const panHandlers = usePanHandlers(c);
  const { trackRef, onLayout } = useTrackLayout(c);
  const onAccessibilityAction = useA11yAction(c);
  return { trackRef, panHandlers, onLayout, onAccessibilityAction };
}

function percentOf(progress: Animated.Value) {
  return progress.interpolate({ inputRange: [0, 1], outputRange: ['0%', '100%'] });
}

function displayLabelMs(c: ScrubCtx, isDragging: boolean, labelMs: number, positionMs: number) {
  return isDragging || c.isHoldingSeek.current ? labelMs : positionMs;
}

export function useScrubberGesture(props: UseScrubberGestureProps) {
  const { ctx, isDragging, labelMs } = useScrubCtx(props);
  useFollowPosition(ctx, props.positionMs, props.durationMs);
  const touch = useTouch(ctx);
  const fill = percentOf(ctx.progress);
  const label = displayLabelMs(ctx, isDragging, labelMs, props.positionMs);
  return { ...touch, isDragging, labelMs: label, fillWidth: fill, thumbLeft: percentOf(ctx.progress) };
}
