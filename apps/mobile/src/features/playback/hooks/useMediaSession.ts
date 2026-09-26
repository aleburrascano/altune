import { useEffect, useRef } from 'react';

import type { PlaybackContextValue, PlaybackTrack } from '@shared/playback/types';

import { usePlaybackRateStore } from '../playbackRateStore';

const SEEK_STEP_SECONDS = 10;
const POSITION_STATE_THROTTLE_MS = 1000;

function trackMetadata(track: PlaybackTrack): MediaMetadata {
  const artwork = track.artworkUrl ? [{ src: track.artworkUrl, sizes: '512x512', type: '' }] : [];
  return new MediaMetadata({ title: track.title, artist: track.artist, artwork });
}

function browserPlaybackState(track: PlaybackTrack | null, status: PlaybackContextValue['status']): MediaSessionPlaybackState {
  if (!track) return 'none';
  return status === 'playing' ? 'playing' : 'paused';
}

function syncMetadataAndState(session: MediaSession, track: PlaybackTrack | null, status: PlaybackContextValue['status']): void {
  session.metadata = track ? trackMetadata(track) : null;
  session.playbackState = browserPlaybackState(track, status);
}

function syncPositionState(session: MediaSession, positionMs: number, durationMs: number, rate: number): void {
  if (!session.setPositionState) return;
  if (!Number.isFinite(durationMs) || durationMs <= 0) return;
  const position = Math.min(positionMs, durationMs) / 1000;
  session.setPositionState({ duration: durationMs / 1000, position, playbackRate: rate });
}

function seekOffsetMs(details: MediaSessionActionDetails): number {
  return (details.seekOffset ?? SEEK_STEP_SECONDS) * 1000;
}

function seekTo(playback: PlaybackContextValue, details: MediaSessionActionDetails): void {
  if (details.seekTime == null) return;
  playback.seekTo(details.seekTime * 1000);
}

function seekBy(playback: PlaybackContextValue, deltaMs: number): void {
  playback.seekTo(Math.max(0, playback.positionMs + deltaMs));
}

type ActionEntry = [MediaSessionAction, MediaSessionActionHandler];

function transportEntries(latest: { current: PlaybackContextValue }): ActionEntry[] {
  return [
    ['play', () => latest.current.resume()],
    ['pause', () => latest.current.pause()],
    ['previoustrack', () => void latest.current.skipPrevious()],
    ['nexttrack', () => void latest.current.skipNext()],
  ];
}

function seekEntries(latest: { current: PlaybackContextValue }): ActionEntry[] {
  return [
    ['seekto', (details) => seekTo(latest.current, details)],
    ['seekbackward', (details) => seekBy(latest.current, -seekOffsetMs(details))],
    ['seekforward', (details) => seekBy(latest.current, seekOffsetMs(details))],
  ];
}

function actionEntries(latest: { current: PlaybackContextValue }): ActionEntry[] {
  return [...transportEntries(latest), ...seekEntries(latest)];
}

function registerActionHandlers(session: MediaSession, entries: ActionEntry[]): () => void {
  for (const [action, onAction] of entries) session.setActionHandler(action, onAction);
  return () => {
    for (const [action] of entries) session.setActionHandler(action, null);
  };
}

function defaultSession(): MediaSession | undefined {
  return typeof navigator === 'object' && 'mediaSession' in navigator
    ? navigator.mediaSession
    : undefined;
}

function useLatestPlayback(playback: PlaybackContextValue): { current: PlaybackContextValue } {
  const latest = useRef(playback);
  useEffect(() => {
    latest.current = playback;
  });
  return latest;
}

function useMediaSessionActions(session: MediaSession | undefined, latest: { current: PlaybackContextValue }): void {
  useEffect(() => {
    if (!session) return undefined;
    return registerActionHandlers(session, actionEntries(latest));
  }, [session, latest]);
}

function useMediaSessionMetadata(session: MediaSession | undefined, playback: PlaybackContextValue): void {
  const { track, status } = playback;
  useEffect(() => {
    if (session) syncMetadataAndState(session, track, status);
  }, [session, track, status]);
}

function isDueForSync(lastSyncRef: { current: number }, now: number): boolean {
  if (now - lastSyncRef.current < POSITION_STATE_THROTTLE_MS) return false;
  lastSyncRef.current = now;
  return true;
}

function useMediaSessionPosition(session: MediaSession | undefined, playback: PlaybackContextValue): void {
  const rate = usePlaybackRateStore((s) => s.rate);
  const { positionMs, durationMs } = playback;
  const lastSyncRef = useRef(0);
  useEffect(() => {
    if (session && isDueForSync(lastSyncRef, Date.now())) syncPositionState(session, positionMs, durationMs, rate);
  }, [session, positionMs, durationMs, rate]);
}

export function useMediaSession(
  playback: PlaybackContextValue,
  session: MediaSession | undefined = defaultSession(),
): void {
  const latest = useLatestPlayback(playback);
  useMediaSessionActions(session, latest);
  useMediaSessionMetadata(session, playback);
  useMediaSessionPosition(session, playback);
}
