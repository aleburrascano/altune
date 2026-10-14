import TrackPlayer, { Event, State, type Progress } from 'react-native-track-player';

import { reloadWidget, writeCoverFromUrl, writeSnapshot } from '@modules/altune-widget';
import type { PlaybackTrack } from '@shared/playback/playbackTrack';
import type { QueueSource } from '@shared/playback/queueOrder';
import { useQueueStore } from '@shared/playback/queueStore';
import { hasSignedInUser, onIdentityChange, onSignOut } from '@shared/session/signOutCleanup';

const COVER_FILE_NAME = 'cover.jpg';

type CoverState = { artworkUrl: string; written: boolean };

type PlayerStatus = { isPlaying: boolean; positionSec: number; durationSec: number };

const IDLE_PLAYER: PlayerStatus = { isPlaying: false, positionSec: 0, durationSec: 0 };

const SIGNED_OUT = { signedIn: false };
const SIGNED_OUT_JSON = JSON.stringify(SIGNED_OUT);

const COVER_TIMEOUT_MS = 15_000;

export function startWidgetSnapshotPublisher(): () => void {
  const publisher = new SnapshotPublisher();
  const unsubscribes = publisher.subscribe();
  publisher.request();
  return () => {
    publisher.stop();
    unsubscribes.forEach((unsubscribe) => unsubscribe());
  };
}

class SnapshotPublisher {
  private stopped = false;
  private lastWrittenJson: string | null = null;
  private cover: CoverState | null = null;
  private coverWrites: Promise<unknown> = Promise.resolve();
  private running = false;
  private requested = false;

  readonly request = (): void => {
    if (this.stopped) return;
    this.requested = true;
    if (this.running) return;
    this.running = true;
    void this.publishWhileRequested();
  };

  readonly clear = (): void => {
    this.cover = null;
    this.writeIfChanged(SIGNED_OUT_JSON);
  };

  subscribe(): (() => void)[] {
    return [
      useQueueStore.subscribe(this.request),
      TrackPlayer.addEventListener(Event.PlaybackState, this.request).remove,
      onIdentityChange(() => queueMicrotask(this.request)),
      onSignOut(this.clear),
    ];
  }

  stop(): void {
    this.stopped = true;
  }

  private publishWhileRequested(): Promise<void> {
    this.requested = false;
    return this.publishCurrent()
      .catch(warnPublishFailed)
      .then(() => this.publishAgainOrStop());
  }

  private publishAgainOrStop(): Promise<void> | void {
    if (this.requested) return this.publishWhileRequested();
    this.running = false;
  }

  private async publishCurrent(): Promise<void> {
    const track = hasSignedInUser() ? useQueueStore.getState().currentTrack() : null;
    this.writeIfChanged(JSON.stringify(await this.snapshotOf(track)));
    if (track !== null) this.downloadCoverOnce(track.artworkUrl);
  }

  private async snapshotOf(track: PlaybackTrack | null): Promise<object> {
    if (!hasSignedInUser()) return SIGNED_OUT;
    if (track === null) return { signedIn: true, nowPlaying: null };
    const player = await playerStatus();
    return { signedIn: true, nowPlaying: nowPlaying(track, player, this.writtenCoverFor(track)) };
  }

  private writtenCoverFor({ artworkUrl }: PlaybackTrack): string | null {
    return this.hasWrittenCover(artworkUrl) ? COVER_FILE_NAME : null;
  }

  private hasWrittenCover(artworkUrl: string | null): boolean {
    return this.cover?.artworkUrl === artworkUrl && this.cover.written;
  }

  private downloadCoverOnce(artworkUrl: string | null): void {
    if (artworkUrl === null || !this.needsCover(artworkUrl)) return;
    const attempt: CoverState = { artworkUrl, written: false };
    this.cover = attempt;
    this.coverWrites = this.coverWrites.then(() => this.writeUnlessSuperseded(attempt));
    void this.coverWrites.then(() => this.republishWhenWritten(attempt));
  }

  private needsCover(artworkUrl: string): boolean {
    return !this.stopped && this.cover?.artworkUrl !== artworkUrl;
  }

  private writeUnlessSuperseded(attempt: CoverState): Promise<void> | void {
    if (this.isSuperseded(attempt)) return;
    return writeCover(attempt.artworkUrl).then((written) => {
      attempt.written = written;
    });
  }

  private republishWhenWritten(attempt: CoverState): void {
    if (this.isSuperseded(attempt) || !attempt.written) return;
    if (isStillCurrent(attempt.artworkUrl)) this.request();
  }

  private isSuperseded(attempt: CoverState): boolean {
    return this.stopped || this.cover !== attempt;
  }

  private writeIfChanged(json: string): void {
    if (this.stopped || json === this.lastWrittenJson) return;
    writeSnapshot(json);
    this.lastWrittenJson = json;
    reloadWidget();
  }
}

function isStillCurrent(artworkUrl: string): boolean {
  return hasSignedInUser() && useQueueStore.getState().currentTrack()?.artworkUrl === artworkUrl;
}

function writeCover(artworkUrl: string): Promise<boolean> {
  return withTimeout(writeCoverFromUrl(artworkUrl, COVER_FILE_NAME), COVER_TIMEOUT_MS).then(
    () => true,
    (error: unknown) => {
      warnPublishFailed(error);
      return false;
    },
  );
}

function withTimeout(work: Promise<void>, ms: number): Promise<void> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timedOut = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error('cover write timed out')), ms);
  });
  return Promise.race([work, timedOut]).finally(() => clearTimeout(timer));
}

function nowPlaying(track: PlaybackTrack, player: PlayerStatus, coverFileName: string | null) {
  const { durationSec, ...playing } = player;
  return {
    ...titleAndArtist(track),
    ...queueFields(),
    ...playing,
    durationSec: durationSec > 0 ? durationSec : (track.durationSeconds ?? 0),
    coverFileName,
  };
}

function titleAndArtist({ title, artist }: PlaybackTrack) {
  return { title, artist };
}

function queueFields() {
  const { repeatMode, shuffled, source } = useQueueStore.getState();
  return { source: sourceLabel(source), shuffle: shuffled, repeat: repeatMode };
}

function sourceLabel(source: QueueSource | null): string | null {
  if (source === null) return null;
  if (source.kind === 'playlist') return source.name;
  if (source.kind === 'search') return source.query;
  return 'Library';
}

function playerStatus(): Promise<PlayerStatus> {
  return Promise.all([TrackPlayer.getPlaybackState(), TrackPlayer.getProgress()])
    .then(([{ state }, progress]) => playerStatusOf(state, progress))
    .catch(() => IDLE_PLAYER);
}

function playerStatusOf(state: State, { position, duration }: Progress): PlayerStatus {
  return {
    isPlaying: state === State.Playing,
    positionSec: Math.floor(position),
    durationSec: Math.floor(duration),
  };
}

function warnPublishFailed(error: unknown): void {
  const name = error instanceof Error ? error.name : 'non-Error';
  console.warn(`[widget] snapshot publish failed: ${name}`);
}
